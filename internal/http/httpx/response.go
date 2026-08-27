package httpx

import (
	"encoding/json"
	"net/http"
)

type ResponseStatus string

const (
	StatusSuccess ResponseStatus = "success"
	StatusError   ResponseStatus = "error"
)

type FieldErrors map[string][]string

func (e FieldErrors) Add(field, message string) {
	e[field] = append(e[field], message)
}

type Pagination struct {
	Current    int64 `json:"current"`
	PageSize   int64 `json:"pageSize"`
	TotalPages int64 `json:"totalPages"`
	TotalItems int64 `json:"totalItems"`
}

func NewPagination(current, pageSize, totalItems int64) Pagination {
	var totalPages int64
	if pageSize > 0 && totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}
	return Pagination{
		Current:    current,
		PageSize:   pageSize,
		TotalPages: totalPages,
		TotalItems: totalItems,
	}
}

type Response struct {
	Status     ResponseStatus `json:"status"`
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Data       any            `json:"data"`
	Pagination *Pagination    `json:"pagination,omitempty"`
	Errors     FieldErrors    `json:"errors"`
}

func WriteSuccess(w http.ResponseWriter, status int, code, message string, data any) error {
	return WriteJSON(w, status, Response{
		Status:  StatusSuccess,
		Code:    code,
		Message: message,
		Data:    data,
		Errors:  nil,
	})
}

func WriteSuccessWithPagination(w http.ResponseWriter, status int, code, message string, data any, pagination Pagination) error {
	return WriteJSON(w, status, Response{
		Status:     StatusSuccess,
		Code:       code,
		Message:    message,
		Data:       data,
		Pagination: &pagination,
		Errors:     nil,
	})
}

func WriteError(w http.ResponseWriter, status int, code, message string, fieldErrors FieldErrors) error {
	return WriteJSON(w, status, Response{
		Status:  StatusError,
		Code:    code,
		Message: message,
		Data:    nil,
		Errors:  fieldErrors,
	})
}

func WriteJSON(w http.ResponseWriter, status int, response Response) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}

	data = append(data, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(data)
	return err
}
