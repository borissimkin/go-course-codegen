package main

import (
	"net/http"
)

// MyApi
func (h *MyApi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {

	case "/user/profile":
		h.handlerProfile

	case "/user/create":
		h.handlerCreate

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// OtherApi
func (h *OtherApi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {

	case "/user/create":
		h.handlerCreate

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}
