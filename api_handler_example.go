package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// SomeStructName (структура на которой нашли apigen)

type response struct {
	Response json.RawMessage `json:"response"`
	Error    string          `json:"error"`
}

func checkToken(token string) bool {
	return token == "100500"
}

func getErrorResponse(err string) []byte {
	data, _ := json.Marshal(response{
		Error: err,
	})

	return data
}

func (h *MyApi) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/user/profile":
		h.handlerProfile(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write(getErrorResponse("unknown method"))
	}
}

func (h *MyApi) handlerProfile(w http.ResponseWriter, r *http.Request) {
	// проверка на метод?
	method := http.MethodPost // или http.MethodGet
	if r.Method != method {
		w.WriteHeader(http.StatusNotAcceptable)
		w.Write(getErrorResponse("bad method"))
		return
	}

	// проверка на авторизацию
	token := r.Header.Get("X-Auth")
	if !checkToken(token) {
		w.WriteHeader(http.StatusForbidden)
		w.Write(getErrorResponse("unauthorized"))
		return
	}

	params := ProfileParams{}

	// цикл по GeneratedParamsField

	// {{FieldName}} := r.FormValue("{{ParamName}}")
	login := r.FormValue("login")

	// default
	if login == "" {
		login = "default value"
	}

	// required
	if login == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("login must me not empty"))
		return
	}

	// enum
	if !(login == "test1" || login == "test2") {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("login must be one of [test1, test2]"))
		return
	} // и тд

	// new enum
	// loginEnums := []string{"test1", "test2"}
	// loginEnumOk := slices.Contains(loginEnums, login)
	// if !loginEnumOk {
	// 	w.WriteHeader(http.StatusBadRequest)
	// 	w.Write(getErrorResponse("login must be one of [test1, test2]"))
	// 	return
	// }

	// min
	if len(login) < 10 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("login len must be >= 10"))
		return
	}

	// max
	if len(login) > 10 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("login len must be <= 10"))
		return
	}
	params.Login = login

	// для инта что у нас может быть
	fieldName := r.FormValue("fieldName")

	fieldNameValue, err := strconv.Atoi(fieldName)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("fieldName must be int"))
	}
	// default
	if fieldNameValue == 0 {
		fieldNameValue = 123
	}

	// required
	if fieldNameValue == 0 {

	}

	// завершение валидации
	ctx := context.Background()

	result := response{}

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
