package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strings"
)


func getGeneratedFuncs(node *ast.File) []*ast.FuncDecl {
	genFuncs := make([]*ast.FuncDecl, 0)

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

		needCodegen := false
		for _, comment := range f.Doc.List {
			needCodegen = needCodegen || strings.HasPrefix(comment.Text, "// apigen:api")
		}

		if !needCodegen {
			fmt.Printf("SKIP method %#v doesnt have apigen mark\n", f.Name.Name)
			continue
		}

		fmt.Println(f)
		genFuncs = append(genFuncs, f)
	}

	return genFuncs
}

func main2() {
	fset := token.NewFileSet()
	// node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	node, err := parser.ParseFile(fset, "../api.go", nil, parser.ParseComments)
	fmt.Println(node, err)

	if err != nil {
		log.Fatal(err)
	}

	// out, _ := os.Create(os.Args[2])
	out, _ := os.Create("api_handlers.go")

	fmt.Println(out, `package `+node.Name.Name)
	fmt.Fprintln(out)

	genFuncs := getGeneratedFuncs(node)

	genStructs := make([]*ast.GenDecl, 0)

	for _, decl := range node.Decls {
		g, ok := decl.(*ast.GenDecl)

		if !ok {
			fmt.Printf("SKIP %#T is not &ast.GenDecl\n", decl)
			continue
		}

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

			// for _, field := currStruct.Fields.List {

			// }
		}

		fmt.Println(g)
		genStructs = append(genStructs, g)
	}

	fmt.Println(genFuncs, genStructs)
}

// код писать тут
