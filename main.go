package main

import (
	"fmt"
	"net/http"
)

func main() {
	// будет вызван метод ServeHTTP у структуры MyApi
	http.Handle("/user/", NewMyApi())

	fmt.Println("starting server at :8080")
	http.ListenAndServe(":8080", nil)
}
