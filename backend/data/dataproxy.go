package data

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"

	"github.com/sirupsen/logrus"

	"reflect"
)

var Manager = &dataManager{}

type dataManager struct {
	Master *sql.DB
	Slave  *sql.DB
}

type LocalTx struct {
	*sql.Tx
}

type CacheHandle interface {
	Get(key string) (reply interface{}, err error)
	Set(key string, val interface{}) (reply interface{}, err error)
	Del(key string) (reply interface{}, err error)
	Close()
}

type TableHandler interface {
	GetStringKey() string
	Decode(v []byte) error
	Encode() []byte
	UpdateSql() (string, []interface{})
	InsertSql() (string, []interface{})
	TableName() string
	SelectSql() (string, []interface{})
}

func (data *dataManager) Begin() (*LocalTx, error) {
	tx, err := data.Master.Begin()
	if err != nil {
		return nil, err
	}
	return &LocalTx{tx}, nil
}

func (tx *LocalTx) TxExec(ctx context.Context, sql string, args ...interface{}) (sql.Result, error) {
	return tx.ExecContext(ctx, sql, args...)
}

func (tx *LocalTx) TxQueryContext(ctx context.Context, resp interface{}, sql string, args ...interface{}) error {
	return queryContext(ctx, nil, tx.Tx, resp, sql, args...)
}

func (tx *LocalTx) TxQuery(resp interface{}, sql string, args ...interface{}) error {
	return queryContext(context.Background(), nil, tx.Tx, resp, sql, args...)
}

func (data *dataManager) Exec(ctx context.Context, sql string, args ...interface{}) (sql.Result, error) {
	return data.Master.ExecContext(ctx, sql, args...)
}

func (data *dataManager) InsertTable(ctx context.Context, resp TableHandler) (sql.Result, error) {
	sql, args := resp.InsertSql()
	res, err := data.Master.ExecContext(ctx, sql, args...)
	if err != nil {
		return res, err
	}
	return res, err
}

func (data *dataManager) UpdateTable(ctx context.Context, resp TableHandler) (sql.Result, error) {
	sql, args := resp.UpdateSql()
	res, err := data.Master.ExecContext(ctx, sql, args...)
	if err != nil {
		return res, err
	}
	return res, err
}

func (data *dataManager) QueryTable(ctx context.Context, resp TableHandler) error {
	sql, args := resp.SelectSql()

	err := queryContext(ctx, data.Slave, nil, resp, sql, args...)
	if err != nil {
		return err
	}
	return err
}

func (data *dataManager) QueryContext(ctx context.Context, resp interface{}, sql string, args ...interface{}) error {
	return queryContext(ctx, data.Slave, nil, resp, sql, args...)
}

func (data *dataManager) Query(resp interface{}, sql string, args ...interface{}) error {
	return queryContext(context.Background(), data.Slave, nil, resp, sql, args...)
}

func (data *dataManager) Close() {
	data.Master.Close()
	if data.Slave != data.Master {
		data.Slave.Close()
	}
}

func MakeSelectSql(selectFields string, fromTable string, where string) string {
	buffer := bytes.Buffer{}
	buffer.WriteString("select ")
	buffer.WriteString(selectFields)
	buffer.WriteString(" from ")
	buffer.WriteString(fromTable)
	buffer.WriteString(" ")
	buffer.WriteString(where)

	return buffer.String()
}

func fillFieldAddr(columnNames []string, val reflect.Value) []interface{} {
	typ := val.Type()

	retAddr := make([]interface{}, 0)
	fieldNum := typ.NumField()
	for _, name := range columnNames {
		for i := 0; i < fieldNum; i++ {
			if !val.Field(i).CanSet() {
				continue
			}
			if tag, ok := typ.Field(i).Tag.Lookup("sql"); ok {
				tags := strings.Split(tag, ",")
				if strings.ToLower(tags[0]) == strings.ToLower(name) {
					retAddr = append(retAddr, val.Field(i).Addr().Interface())
					break
				}
			} else {
				if camelToUnderscore(typ.Field(i).Name) == name {
					retAddr = append(retAddr, val.Field(i).Addr().Interface())
					break
				}
			}
		}
	}
	return retAddr
}

func parseRow(rows *sql.Rows, columnNames []string, val reflect.Value) error {
	for rows.Next() {
		scan := fillFieldAddr(columnNames, val)
		logrus.Infoln(scan)
		return rows.Scan(scan...)
	}
	return rows.Err()
}

func parseRows(rows *sql.Rows, columnNames []string, val reflect.Value) error {
	typ := val.Type()
	t := typ.Elem()

	if t.Kind() != reflect.Slice {
		return fmt.Errorf("scan rows typr not slice (%v)", t.Kind())
	}
	results := reflect.MakeSlice(t, 0, 0)
	t = t.Elem()

	isPtr := false
	if t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct {
		isPtr = true
		t = t.Elem() // struct
	}
	if t.Kind() != reflect.Struct {
		return fmt.Errorf("scan data invalid(%v)", t.Kind())
	}

	for rows.Next() {
		row := reflect.New(t).Elem()
		scans := fillFieldAddr(columnNames, row)
		err := rows.Scan(scans...)
		if err != nil {
			return err
		}

		if isPtr {
			results = reflect.Append(results, row.Addr())
			continue
		}
		results = reflect.Append(results, row)
	}

	val.Elem().Set(results)
	return rows.Err()
}

func queryContext(ctx context.Context, db *sql.DB, tx *sql.Tx, resp interface{}, sqlStr string, args ...interface{}) error {
	var rows *sql.Rows
	var err error
	if tx == nil {
		rows, err = db.QueryContext(ctx, sqlStr, args...)
	} else {
		rows, err = tx.QueryContext(ctx, sqlStr, args...)
	}
	if err != nil {
		return err
	}

	columnNames, err := rows.Columns()
	if err != nil {
		return err
	}
	val := reflect.ValueOf(resp)
	typ := val.Type()

	if typ.Kind() != reflect.Ptr {
		return fmt.Errorf("scan data must ptr %s", typ.Kind())
	}

	if typ.Elem().Kind() == reflect.Struct {
		return parseRow(rows, columnNames, val.Elem())
	} else {
		return parseRows(rows, columnNames, val)
	}
}

// 驼峰转下划线
func camelToUnderscore(name string) string {
	buf := make([]rune, 0, len(name)+4)
	var preIsUpper bool
	for i, r := range name {
		if unicode.IsUpper(r) {
			if i != 0 && !preIsUpper {
				buf = append(buf, '_')
			}
			buf = append(buf, unicode.ToLower(r))
		} else {
			buf = append(buf, r)
		}
		preIsUpper = unicode.IsUpper(r)
	}
	return string(buf)
}
