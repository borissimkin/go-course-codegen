package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

type serveHTTPTempl struct {
	ReceiverTypeName string
	GeneratedFuncs   []GeneratedFunc
}

type checkMethodTempl struct {
	HttpMethod string
}

type responseTempl struct {
	FuncName string
}

type baseValidationTempl struct {
	FieldName string
	VarName   string
}

type validationDefaultStringTempl struct {
	baseValidationTempl
	DefaultValue string
}

type validationDefaultIntTempl struct {
	baseValidationTempl
	DefaultValue int
}

type validationRequiredTempl struct {
	baseValidationTempl
	EmptyValue string
}

type validationEnumTempl struct {
	baseValidationTempl
	Enum []string
}

type validationMinMaxTempl struct {
	baseValidationTempl
	WithLen bool
	Value   int
	IsMin   bool
}

type validationCastToIntTempl struct {
	baseValidationTempl
}

var (
	serveHTTPTemplate = template.Must(template.New("serveHTTPTempl").Parse(`
	// {{.ReceiverTypeName}}
func (h *{{.ReceiverTypeName}}) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
		{{range $val := .GeneratedFuncs}}
		case "{{$val.Url}}":
			h.handler{{$val.FuncName}}(w, r)
		{{end}}
		default:
			w.WriteHeader(http.StatusNotFound)
	}
}

`))

	checkMethodTemplate = template.Must(template.New("checkMethodTempl").Parse(`
	// check http method
	method := "{{.HttpMethod}}"
	if r.Method != method {
		w.WriteHeader(http.StatusNotAcceptable)
		w.Write(getErrorResponse("bad method"))
		return
	}
	`))

	checkAuthTemplate = template.Must(template.New("checkAuthTempl").Parse(`
	// check auth
	token := r.Header.Get("X-Auth")
	if !checkToken(token) {
		w.WriteHeader(http.StatusForbidden)
		w.Write(getErrorResponse("unathorized"))
		return
	}
	`))

	validationCastToIntTemplate = template.Must(template.New("validationCastToIntTempl").Parse(`
	// cast to int
	{{.VarName}}, err := strconv.Atoi({{.FieldName}})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("{{.FieldName}} must be int"))
	}
	`))

	validationDefaultStringTemplate = template.Must(template.New("validationDefaultStringTempl").Parse(`
	// default
	if {{.VarName}} == "" {
		{{.VarName}} = "{{.DefaultValue}}"
	}
	`))

	validationDefaultIntTemplate = template.Must(template.New("validationDefaultIntTempl").Parse(`
	// default
	if {{.VarName}} == 0 {
		{{.VarName}} = {{.DefaultValue}}
	}
	`))

	validationMinMaxTemplate = template.Must(template.New("validationMinMaxTempl").Parse(`
	// {{if $.IsMin}}min{{else}}max{{end}}
	if {{ if $.WithLen }}len({{.VarName}}){{ else }}{{.VarName}}{{ end }} {{ if $.IsMin }}< {{ else }}> {{ end }}{{$.Value}} {
    	w.WriteHeader(http.StatusBadRequest)
    	w.Write(getErrorResponse("{{.FieldName}}{{if $.WithLen}} len{{end}} must be {{if $.IsMin}}>= {{else}}<= {{end}}{{$.Value}}"))
    	return
	}
	`))

	validationRequiredTemplate = template.Must(template.New("validationRequiredTempl").Parse(`
	// required
	if {{.VarName}} == {{.EmptyValue}} {
		w.WriteHeader(http.StatusBadRequest)
		w.Write(getErrorResponse("{{.FieldName}} must me not empty"))
		return
	}
	`))

	// todo: не робит цикл
	validationEnumTemplate = template.Must(template.New("validationEnumTempl").Parse(`
	// enum
	if !(
    	{{- range $i, $v := .Enum -}}
        	{{- if gt $i 0}} || {{end -}}
        	{{$.VarName}} == "{{$v}}"
    	{{- end -}}
	) {
    	w.WriteHeader(http.StatusBadRequest)
    	w.Write(getErrorResponse("{{.FieldName}} must be one of [
        	{{- range $i, $v := .Enum -}}
            	{{- if gt $i 0}}, {{end -}}
            	{{$v}}
        	{{- end -}}
    	]"))
    	return
	}
	`))

	responseTemplate = template.Must(template.New("responseTempl").Parse(`
	ctx := context.Background()

	result := response{}

	resp, err := h.{{.FuncName}}(ctx, params)
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
	`))
)

var (
	apiGenPrefix       = "// apigen:api "
	validatorRequired  = "required"
	validatorParamName = "paramname"
	validatorEnum      = "enum"
	validatorMin       = "min"
	validatorMax       = "max"
	validatorDefault   = "default"
)

type ApiGenApi struct {
	Url    string `json:"url"`
	Auth   bool   `json:"auth"`
	Method string `json:"method"`
}

type GeneratedFunc struct {
	Receiver         *ast.FieldList
	ReceiverTypeName string
	FuncName         string

	InTypeName string // remove?
	In         *ast.Field

	Url    string
	Auth   bool
	Method string
}

type ValidatorValue[T any] struct {
	Value T
	Exist bool
}

func NewValidatorValue[T any](value T) ValidatorValue[T] {
	return ValidatorValue[T]{
		Value: value,
		Exist: true,
	}
}

type GeneratedParamsField struct {
	FieldName string
	FieldType string

	DefaultValue ValidatorValue[string]
	Required     ValidatorValue[bool]
	Enum         ValidatorValue[[]string]
	ParamName    ValidatorValue[string]
	Min          ValidatorValue[int]
	Max          ValidatorValue[int]
}

type GeneratedStruct struct {
	Name       string
	Attributes []GeneratedParamsField
}

func getValidatorValue(tagValue string) string {
	val := strings.Split(tagValue, "=")

	return val[1]
}

func getValidatorEnumValue(enumValue string) []string {
	return strings.Split(enumValue, "|")
}

func getGeneratedParamsField(tagValue string) GeneratedParamsField {
	params := GeneratedParamsField{}

	values := strings.Split(tagValue, ",")

	for _, v := range values {
		field := strings.Split(v, "=")[0]

		if field == validatorRequired {
			params.Required = NewValidatorValue(true)
		}

		if field == validatorParamName {
			val := getValidatorValue(v)
			params.ParamName = NewValidatorValue(val)
		}

		if field == validatorEnum {
			val := getValidatorValue(v)
			values := getValidatorEnumValue(val)
			params.Enum = NewValidatorValue(values)
		}

		if field == validatorDefault {
			val := getValidatorValue(v)
			params.DefaultValue = NewValidatorValue(val)
		}

		if field == validatorMin {
			val := getValidatorValue(v)

			min, err := strconv.Atoi(val)
			if err != nil {
				panic(err)
			}
			params.Min = NewValidatorValue(min)
		}

		if field == validatorMax {
			val := getValidatorValue(v)

			max, err := strconv.Atoi(val)
			if err != nil {
				panic(err)
			}

			params.Max = NewValidatorValue(max)
		}
	}

	return params
}

func getGeneratedFuncs(node *ast.File) []GeneratedFunc {
	genFuncs := make([]GeneratedFunc, 0)

	for _, decl := range node.Decls {
		f, ok := decl.(*ast.FuncDecl)

		if !ok {
			fmt.Printf("SKIP %#T is not &ast.FuncDecl\n", decl)
			continue
		}

		if f.Doc == nil {
			fmt.Printf("SKIP method %#v doesnt have comments\n", f.Name.Name)
			continue
		}

		apiGenStr := ""
		for _, comment := range f.Doc.List {
			str, found := strings.CutPrefix(comment.Text, apiGenPrefix)
			if found {
				apiGenStr = str
				break
			}
		}

		if apiGenStr == "" {
			fmt.Printf("SKIP method %#v doesnt have apigen mark\n", f.Name.Name)
			continue
		}

		apiGen := &ApiGenApi{}
		err := json.Unmarshal([]byte(apiGenStr), apiGen)
		if err != nil {
			panic(err)
		}

		generatedFunc := GeneratedFunc{
			Method:   apiGen.Method,
			Url:      apiGen.Url,
			Auth:     apiGen.Auth,
			FuncName: f.Name.Name,
			Receiver: f.Recv,
		}

		for _, field := range f.Recv.List {
			fieldType := field.Type.(*ast.StarExpr)
			fieldName := fieldType.X.(*ast.Ident).Name
			fmt.Println(fieldName)
			generatedFunc.ReceiverTypeName = fieldName
		}

		for index, param := range f.Type.Params.List {
			if index > 0 {
				generatedFunc.In = param
				generatedFunc.InTypeName = param.Type.(*ast.Ident).Name
			}
		}

		genFuncs = append(genFuncs, generatedFunc)
	}

	return genFuncs
}

func getGeneratedStructs(node *ast.File) []GeneratedStruct {
	genStructs := make([]GeneratedStruct, 0)
	apiValidatorReg := regexp.MustCompile(`apivalidator:"([^"]+)"`)

	for _, decl := range node.Decls {
		g, ok := decl.(*ast.GenDecl)

		if !ok {
			fmt.Printf("SKIP %#T is not &ast.GenDecl\n", decl)
			continue
		}

		// todo: нужны метки на циклы, чтобы понять в самом нижнем цикле что валидация по итогу не нужна?

		fmt.Println(g)

		// SPECS_LOOP:
		for _, spec := range g.Specs {
			currType, ok := spec.(*ast.TypeSpec)
			if !ok {
				fmt.Printf("SKIP %#T is not ast.TypeSpec\n", spec)
				continue
			}

			currStruct, ok := currType.Type.(*ast.StructType)
			if !ok {
				fmt.Printf("SKIP %#T is not ast.StructType\n", currStruct)
				continue
			}

			fmt.Println(currType.Name.Name)

			genStruct := GeneratedStruct{
				Name: currType.Name.Name,
			}

			needCodegen := false
			for _, field := range currStruct.Fields.List {
				tag := field.Tag

				if tag == nil {
					continue
				}

				match := apiValidatorReg.FindStringSubmatch(tag.Value)

				if len(match) > 1 {
					needCodegen = needCodegen || true
					validatorValueString := match[1]
					generatedParams := getGeneratedParamsField(validatorValueString)

					for _, name := range field.Names {
						generatedParams.FieldName = name.Name
					}

					t, ok := field.Type.(*ast.Ident)
					if ok {
						generatedParams.FieldType = t.Name
					}

					genStruct.Attributes = append(genStruct.Attributes, generatedParams)
					fmt.Println(generatedParams)
				}
			}

			if needCodegen {
				genStructs = append(genStructs, genStruct)
			}
		}
	}

	return genStructs
}

func writeImports(out io.Writer) {
	fmt.Fprintln(out, `
	import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)`)
}

func writeUtils(out io.Writer) {
	fmt.Fprintln(out, "type response struct {\n\tResponse json.RawMessage `json:\"response\"`\n\tError    string          `json:\"error\"`\n}\n\nfunc checkToken(token string) bool {\n\treturn token == \"100500\"\n}\n\nfunc getErrorResponse(err string) []byte {\n\tdata, _ := json.Marshal(response{\n\t\tError: err,\n\t})\n\n\treturn data\n}")
}

func generateValidationCode(out io.Writer, genStruct *GeneratedStruct) {
	for _, attr := range genStruct.Attributes {

		fieldName := strings.ToLower(attr.FieldName)
		paramName := fieldName
		fieldNameInt := fieldName + "Int"

		baseValidation := baseValidationTempl{
			FieldName: fieldName,
			VarName:   fieldName,
		}

		if attr.FieldType == "int" {
			baseValidation.VarName = fieldNameInt
		}

		if attr.ParamName.Exist {
			paramName = attr.ParamName.Value
		}

		fmt.Fprintf(out, "\n	%v := r.FormValue(\"%v\")\n", fieldName, paramName)

		if attr.FieldType == "int" {
			validationCastToIntTemplate.Execute(out, validationCastToIntTempl{
				baseValidationTempl: baseValidation,
			})
		}

		if attr.DefaultValue.Exist {
			switch attr.FieldType {
			case "string":
				validationDefaultStringTemplate.Execute(out, validationDefaultStringTempl{
					baseValidationTempl: baseValidation,
					DefaultValue:        attr.DefaultValue.Value,
				})
			case "int":
				defaultValue, _ := strconv.Atoi(attr.DefaultValue.Value)

				validationDefaultIntTemplate.Execute(out, validationDefaultIntTempl{
					baseValidationTempl: baseValidation,
					DefaultValue:        defaultValue,
				})
			}
		}

		if attr.Required.Exist {
			emptyValue := ``
			switch attr.FieldType {
			case "string":
				emptyValue = `""`
				validationRequiredTemplate.Execute(out, validationRequiredTempl{
					baseValidationTempl: baseValidation,
					EmptyValue:          emptyValue,
				})
			case "int":
				emptyValue = `0`
				validationRequiredTemplate.Execute(out, validationRequiredTempl{
					baseValidationTempl: baseValidation,
					EmptyValue:          emptyValue,
				})
			}
		}

		if attr.Min.Exist {
			validationMinMaxTemplate.Execute(out, validationMinMaxTempl{
				baseValidationTempl: baseValidation,
				WithLen:             attr.FieldType == "string",
				Value:               attr.Max.Value,
				IsMin:               true,
			})
		}

		if attr.Max.Exist {
			validationMinMaxTemplate.Execute(out, validationMinMaxTempl{
				baseValidationTempl: baseValidation,
				WithLen:             attr.FieldType == "string",
				Value:               attr.Max.Value,
				IsMin:               false,
			})
		}

		if attr.Enum.Exist {
			validationEnumTemplate.Execute(out, validationEnumTempl{
				baseValidationTempl: baseValidation,
				Enum:                attr.Enum.Value,
			})
		}

		fmt.Fprintf(out, "\n	params.%v = %v\n", attr.FieldName, fieldName)
	}
}

func generateCode(out io.Writer, funcs []GeneratedFunc, structs []GeneratedStruct) error {
	funcsMap := make(map[string][]GeneratedFunc, len(funcs))

	for _, f := range funcs {
		funcsMap[f.ReceiverTypeName] = append(funcsMap[f.ReceiverTypeName], f)
	}

	for k, v := range funcsMap {
		serveHTTPTemplate.Execute(out, serveHTTPTempl{
			ReceiverTypeName: k,
			GeneratedFuncs:   v,
		})

		for _, f := range v {
			fmt.Fprintf(out, "func (h *%s) handler%s(w http.ResponseWriter, r *http.Request) {\n", k, f.FuncName)

			if f.Method != "" {
				method := http.MethodPost
				if f.Method == http.MethodGet {
					method = http.MethodGet
				}

				checkMethodTemplate.Execute(out, checkMethodTempl{
					HttpMethod: method,
				})
			}

			if f.Auth {
				checkAuthTemplate.Execute(out, nil)
			}

			fmt.Fprintln(out)
			fmt.Fprintf(out, "	params := %v{}\n", f.InTypeName)

			// валидация
			var genStruct *GeneratedStruct
			for _, s := range structs {
				if s.Name == f.InTypeName {
					genStruct = &s
					break
				}
			}

			if genStruct != nil {
				generateValidationCode(out, genStruct)
			}

			// ---

			responseTemplate.Execute(out, responseTempl{
				FuncName: f.FuncName,
			})

			fmt.Fprint(out, "\n}\n\n")
		}

	}

	return nil
}

func main() {
	fset := token.NewFileSet()
	// node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	node, err := parser.ParseFile(fset, "../api.go", nil, parser.ParseComments)
	fmt.Println(node, err)

	if err != nil {
		log.Fatal(err)
	}

	// out, _ := os.Create(os.Args[2])
	out, _ := os.Create("api_handlers.go")

	genFuncs := getGeneratedFuncs(node)

	genStructs := getGeneratedStructs(node)

	fmt.Fprintln(out, `package `+node.Name.Name)
	writeImports(out)
	writeUtils(out)
	generateCode(out, genFuncs, genStructs)
}
