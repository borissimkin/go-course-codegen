package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// SomeStructName (структура на которой нашли apigen)

type Response struct {
	Response json.RawMessage `json:"response"`
	Error    string          `json:"error"`
}

func (h *MyApi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/user/profile":
		h.handlerProfile(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *MyApi) handlerProfile(w http.ResponseWriter, r *http.Request) {
	// проверка на метод?
	// method := http.MethodPost
	// if r.Method != method {
	// 	w.WriteHeader(http.StatusNotAcceptable)
	// 	return
	// }

	// проверка на авторизацию
	// token := r.Header.Get("X-Auth")
	// if token != "100500" {
	// 	w.WriteHeader(http.StatusForbidden)
	// 	return
	// }

	params := ProfileParams{}

	login := r.FormValue("login")
	// required
	// проверка на тип, стринг или инт (соответствующий шаблон)
	if login == "" {
		w.WriteHeader(http.StatusBadRequest)
	}

	params.Login = login

	// завершение валидации
	ctx := context.Background()

	result := Response{}

	resp, err := h.Profile(ctx, params)
	if err != nil {
		apiError := err.(ApiError)

		w.WriteHeader(apiError.HTTPStatus)
		result.Error = apiError.Error()

		data, _ := json.Marshal(result)
		w.Write(data)

		return
	}

	data, _ := json.Marshal(resp)
	result.Response = data

	data, _ = json.Marshal(result)

	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
