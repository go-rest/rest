package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-rest/httprequest"

	"github.com/golang/protobuf/proto"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
	"google.golang.org/appengine"
)

type ServeMux struct {
	router     *httprouter.Router
	middleware []RestFunc
}

func New(options ...RestFunc) *ServeMux {
	r := httprouter.New()
	return &ServeMux{
		router:     r,
		middleware: options,
	}
}

// DefaultServeMux is the default ServeMux.
var DefaultServeMux = New()

func (mux *ServeMux) Handle(path string, v interface{}, options ...RestFunc) {
	m := append(mux.middleware, options...)
	h := handler{impl: v, middleware: m}
	if _, ok := v.(Getter); ok {
		mux.router.GET(path, h.handle)
	}
	if _, ok := v.(Poster); ok {
		mux.router.POST(path, h.handle)
	}
	if _, ok := v.(Putter); ok {
		mux.router.PUT(path, h.handle)
	}
	if _, ok := v.(Deleter); ok {
		mux.router.DELETE(path, h.handle)
	}
}

// Handle registers the handler for the given pattern in the DefaultServeMux. The documentation for ServeMux explains how patterns are matched.
func Handle(path string, v interface{}, options ...RestFunc) {
	DefaultServeMux.Handle(path, v, options...)
}

func (mux *ServeMux) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	mux.router.ServeHTTP(w, req)
}

// RestFunc type is an adapter to allow the use of
// ordinary functions as REST middleware handlers.  If f is a function
// with the appropriate signature, RestFunc(f) is a
// Handler object that calls f.
type RestFunc func(context.Context, *http.Request) (context.Context, error)

type handler struct {
	impl       interface{}
	middleware []RestFunc
}

func (h handler) handle(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	r.ParseForm()
	params := httprequest.Params{Request: r, PathVar: ps}

	ctx := context.Background()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var err error
	for i := range h.middleware {
		ctx, err = h.middleware[i](ctx, r)
		if err != nil {
			serveError(w, err)
			return
		}
	}
	var v interface{}
	if appengine.IsDevAppServer() {
		//appstats.WithContext(ctx, r.Method, r.URL.Path, func(c context.Context) {
		v, err = h.serve(ctx, params)
		//})
	} else {
		v, err = h.serve(ctx, params)
	}

	if err != nil {
		serveError(w, err)
		return
	}
	if b, ok := v.([]byte); ok {
		w.Write(b)
		return
	}
	if s, ok := v.(string); ok {
		w.Write([]byte(s))
		return
	}

	a := strings.ToLower(r.Header.Get("Accept"))
	useProto := strings.Contains(a, "application/x-protobuf")
	if !useProto {
		err := json.NewEncoder(w).Encode(v)
		if err != nil {
			serveError(w, err)
		}
		return
	}
	p, ok := v.(proto.Message)
	//log.Println(p, ok, reflect.TypeOf(v))
	if !ok {
		err := json.NewEncoder(w).Encode(v)
		if err != nil {
			serveError(w, err)
		}
		return
	}

	b, err := proto.Marshal(p)
	if err != nil {
		serveError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(b)
}

func serveError(w http.ResponseWriter, err error) {
	herr, ok := err.(StatusError)
	if !ok {
		herr = NewStatusError(err.Error(), http.StatusBadRequest).(StatusError)
	}
	w.WriteHeader(herr.StatusCode)
	json.NewEncoder(w).Encode(herr)
}

func (h *handler) serve(ctx context.Context, params httprequest.Params) (interface{}, error) {
	impl := reflect.ValueOf(h.impl)
	if impl.Type().Kind() == reflect.Ptr {
		if !impl.IsNil() {
			orig := impl
			impl = reflect.New(impl.Type().Elem())
			impl.Elem().Set(orig.Elem())
		} else {
			impl = reflect.New(impl.Type().Elem())
		}
		var isSlice bool
		switch impl.Elem().Kind() {
		case reflect.Struct:
		case reflect.Slice:
			isSlice = true
		}
		//log.Printf("SERVE %+v", params)
		if err := httprequest.Unmarshal(params, impl.Interface()); err != nil {
			// check if we can unmarshal a single interface from a slice
			if !isSlice {
				return nil, err
			}

			impl.Elem().Set(reflect.MakeSlice(impl.Elem().Type(), 1, 1))
			z := reflect.New(impl.Elem().Index(0).Type())
			if err := httprequest.Unmarshal(params, impl.Interface()); err != nil {
				return nil, err
			}
			impl.Elem().Index(0).Set(reflect.Indirect(z))
		}
	}

	v := impl.Interface()
	switch params.Request.Method {
	case "GET":
		if v, ok := v.(Getter); ok {
			return v.Get(ctx)
		}

	case "POST":
		if v, ok := v.(Poster); ok {
			return v.Post(ctx)
		}

	case "PUT":
		if v, ok := v.(Putter); ok {
			return v.Put(ctx)
		}

	case "DELETE":
		if v, ok := v.(Deleter); ok {
			return v.Delete(ctx)
		}
	}
	return nil, fmt.Errorf("%v unsupported method for %v", params.Request.Method, params.Request.RequestURI)
}

type Getter interface {
	Get(ctx context.Context) (interface{}, error)
}

type Poster interface {
	Post(ctx context.Context) (interface{}, error)
}

type Putter interface {
	Put(ctx context.Context) (interface{}, error)
}

type Deleter interface {
	Delete(ctx context.Context) (interface{}, error)
}

type StatusError struct {
	Err        string `json:"message"`
	StatusCode int    `json:"statusCode"`
}

func (h StatusError) Error() string {
	return h.Err
}

func NewStatusError(err string, statusCode int) error {
	return StatusError{Err: err, StatusCode: statusCode}
}
