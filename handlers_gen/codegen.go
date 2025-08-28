package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
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

// todo: мапа?
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
		if v == validatorRequired {
			params.Required = NewValidatorValue(true)
		}

		if v == validatorParamName {
			val := getValidatorValue(v)
			params.ParamName = NewValidatorValue(val)
		}

		if v == validatorEnum {
			val := getValidatorValue(v)
			values := getValidatorEnumValue(val)
			params.Enum = NewValidatorValue(values)
		}

		if v == validatorDefault {
			val := getValidatorValue(v)
			params.DefaultValue = NewValidatorValue(val)
		}

		if v == validatorMin {
			val := getValidatorValue(v)

			min, err := strconv.Atoi(val)
			if err != nil {
				panic(err)
			}
			params.Min = NewValidatorValue(min)
		}

		if v == validatorMax {
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
)`)
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
	generateCode(out, genFuncs, genStructs)
}
