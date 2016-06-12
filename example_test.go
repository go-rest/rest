package rest

import "golang.org/x/net/context"

type HelloReq struct {
	Name string `rest:"name,path"`
}

func (h *HelloReq) Get(ctx context.Context) (interface{}, error) {
	return "Hello " + h.Name, nil
}

func ExampleServeMux_Handle() {
	mux := New()
	mux.Handle("/:name", new(HelloReq))
}
