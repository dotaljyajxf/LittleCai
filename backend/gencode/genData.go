/**********************************************************************************************************************
 *
 * Copyright (c) 2010 babeltime.com, Inc. All Rights Reserved
 * $
 *
 **********************************************************************************************************************/

/**
 * @file $
 * @author $(liujianyong@babeltime.com)
 * @date $
 * @version $
 * @brief
 *
 **/
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"strings"
	"text/template"
)

const iTableTpl = `
package table

import (
	"encoding/json"
	"fmt"
	"sync"
)

var {{.FileNameNoExt}}Pool = &sync.Pool{New: func() interface{} {
	return new({{.ModuleName}})
}}

func New{{.ModuleName}}() *{{.ModuleName}} {
	ret := {{.FileNameNoExt}}Pool.Get().(*{{.ModuleName}})
	*ret = {{.ModuleName}}{}
	return ret
}

func (this *{{.ModuleName}}) Release() {
	*this = {{.ModuleName}}{}
	{{.FileNameNoExt}}Pool.Put(this)
}

func (this *{{.ModuleName}}) GetStringKey() string {
	return {{.StringKey}}
}

func (this *{{.ModuleName}}) Decode(v []byte) error {
	return json.Unmarshal(v, this)
}

func (this *{{.ModuleName}}) Encode() []byte {
	b, _ := json.Marshal(this)
	return b
}

func (this *{{.ModuleName}}) SelectSql() (string, []interface{}) {
	sql := "{{.SelectStr}}"
	return sql, []interface{}{ {{.SelectRet}} }
}

func (this *{{.ModuleName}}) InsertSql() (string, []interface{}) {
	sql := "{{.InsertStr}}"
	return sql, []interface{}{ {{.InsertRet}} }
}

func (this *{{.ModuleName}}) UpdateSql() (string, []interface{}) {
	sql := "{{.UpdateStr}}"
	return sql, []interface{}{ {{.SelectRet}} }
}

{{$x := .}}
{{range $field := .Fields}}
func (this *{{$x.ModuleName}}) Get{{$field.Name}}() {{$field.Type}} {
	return this.{{$field.Name}}
}

func (this *{{$x.ModuleName}}) Set{{$field.Name}}(a{{$field.Name}} {{$field.Type}}) {
	this.{{$field.Name}} = a{{$field.Name}}
}
{{end}}
`

const iMapTpl = `
package table

var DbMap []interface{} = []interface{}{
{{range $name := .ModuleNames}}
	&{{$name}}{},
{{- end}}
}
`

type Modules struct {
	ModuleNames []string
}

type TableModule struct {
	ModuleName        string
	FileNameNoExt     string
	Fields            []FieldsType
	SelectStr         string
	SelectRet         string
	UpdateStr         string
	InsertStr         string
	InsertRet         string
	FieldName2SqlName map[string]string
	StringKey         string
}

type FieldsType struct {
	Name string
	Type string
}

func (tb *TableModule) makeFileStruct(dir string, fileName string) {
	tk := token.NewFileSet()

	pf, err := parser.ParseFile(tk, dir+"/"+fileName, nil, parser.ParseComments)
	if err != nil {
		fmt.Println("ParseFile failed err : %s", err.Error())
		return
	}

	for _, decl := range pf.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		if len(gd.Specs) > 1 {
			continue
		}

		sp, ok := gd.Specs[0].(*ast.TypeSpec)
		if !ok {
			continue
		}

		tb.ModuleName = sp.Name.Name
		tb.FileNameNoExt = fileName[:len(fileName)-3]
		tb.Fields = make([]FieldsType, 0)
		tb.FieldName2SqlName = make(map[string]string)
		primaryKeys := make([]string, 0)

		st, ok := sp.Type.(*ast.StructType)
		if !ok {
			fmt.Printf("single type not struct")
			continue
		}

		tb.SelectStr = "select "
		tb.UpdateStr = "update " + tb.FileNameNoExt + " set"
		tb.InsertStr = "insert into " + tb.FileNameNoExt + "("
		insertValues := " values("
		for index, fl := range st.Fields.List {
			fident, ok := fl.Type.(*ast.Ident)
			if ok {
				tb.Fields = append(tb.Fields, FieldsType{fl.Names[0].Name, fident.Name})
			}
			tag := fl.Tag.Value
			if strings.Contains(tag, "primary_key") {
				primaryKeys = append(primaryKeys, fl.Names[0].Name)
				parts := strings.Split(tag, "\"")
				firstPart := strings.Split(parts[1], ",")
				tb.FieldName2SqlName[fl.Names[0].Name] = firstPart[0]
			} else {
				parts := strings.Split(tag, "\"")
				tb.FieldName2SqlName[fl.Names[0].Name] = parts[1]
			}

			if strings.HasPrefix(tb.FieldName2SqlName[fl.Names[0].Name], "create_at") ||
				strings.HasPrefix(tb.FieldName2SqlName[fl.Names[0].Name], "update_at") {
				tb.SelectStr += "`" + tb.FieldName2SqlName[fl.Names[0].Name] + "`"
				if index != len(st.Fields.List)-1 {
					tb.SelectStr += ","
				}
				continue
			}

			tb.SelectStr += "`" + tb.FieldName2SqlName[fl.Names[0].Name] + "`"
			tb.UpdateStr += "`" + tb.FieldName2SqlName[fl.Names[0].Name] + "` = ?"
			tb.InsertStr += "`" + tb.FieldName2SqlName[fl.Names[0].Name] + "`"
			insertValues += "?"
			tb.InsertRet += "this." + fl.Names[0].Name
			if index != len(st.Fields.List)-1 {
				tb.SelectStr += ","
				tb.UpdateStr += ","
				tb.InsertStr += ","
				insertValues += ","
				tb.InsertRet += ","
			}
		}
		tb.SelectStr += " from " + tb.FileNameNoExt + " where "
		tb.UpdateStr += " where "
		tb.InsertStr += ") " + insertValues + ")"
		tb.StringKey = `fmt.Sprintf("`
		for index, key := range primaryKeys {
			tb.SelectStr += tb.FieldName2SqlName[key] + " = ?"
			tb.UpdateStr += tb.FieldName2SqlName[key] + " = ?"
			tb.StringKey += `%d`
			tb.SelectRet += "this." + key
			if index != len(primaryKeys)-1 {
				tb.SelectStr += " and "
				tb.SelectRet += ","
				tb.StringKey += "#"
			}
		}
		tb.StringKey += `",` + tb.SelectRet + ")"
	}
}

func (m *Modules) genMap(dataDirPath string) {
	funcMap := template.FuncMap{
		"dec": func(i int) int {
			return i - 1
		},
	}
	t := template.New("templateMap")
	t = t.Funcs(funcMap)
	t, err := t.Parse(iMapTpl)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	fpMap, err := os.OpenFile(dataDirPath+"/map_auto.go",
		os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		fmt.Println("create file error : %s", err.Error())
		return
	}
	err = t.Execute(fpMap, m)
	if err != nil {
		fmt.Println("genMap err : ", err.Error())
		return
	}
	fpMap.Close()
}

func genTableFile() {
	path := os.Getenv("HOME")
	if path == "" {
		fmt.Println("can not get GOPATH")
		return
	}

	dataDirPath := path + "/LittleCai/backend/data/table"

	fd, err := ioutil.ReadDir(dataDirPath)
	if err != nil {
		fmt.Println("read dir error : %s", err.Error())
		return
	}

	funcMap := template.FuncMap{
		"dec": func(i int) int {
			return i - 1
		},
	}
	t := template.New("template")
	t = t.Funcs(funcMap)
	t, err = t.Parse(iTableTpl)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	modules := new(Modules)
	for _, file := range fd {
		if file.IsDir() {
			continue
		}

		if strings.Contains(file.Name(), "_auto") {
			continue
		}

		tb := new(TableModule)
		tb.makeFileStruct(dataDirPath, file.Name())

		//fmt.Printf("%v", *tb)
		fpAuto, err := os.OpenFile(dataDirPath+"/"+tb.FileNameNoExt+"_auto.go",
			os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
		if err != nil {
			fmt.Println("create file error : %s", err.Error())
			return
		}

		err = t.Execute(fpAuto, tb)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		fpAuto.Close()
		modules.ModuleNames = append(modules.ModuleNames, tb.ModuleName)
	}
	//modules.genMap(dataDirPath)
}

func main() {
	genTableFile()
	fmt.Println("done ")
}