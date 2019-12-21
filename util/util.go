package util

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/go-pg/pg"
	"github.com/mayur-tolexo/pg-shifter/model"
)

//variables
var (
	QueryFp *os.File
	Y       = "y"
	Yes     = "yes"
)

//const in histroy
const (
	historyTag = "_history"
)

//GetCompositeUniqueKey : Get composite unique key name and columns
func GetCompositeUniqueKey(tx *pg.Tx, tableName string) (ukSchema []model.UKSchema, err error) {
	query := `
	with comp as (
		select  c.column_name, pgc.conname
		, array_position(pgc.conkey::int[],c.ordinal_position::int) as position
		from pg_constraint as pgc join
		information_schema.table_constraints tc on pgc.conname = tc.constraint_name,
		unnest(pgc.conkey::int[]) as colNo join information_schema.columns as c
		on c.ordinal_position = colNo and c.table_name = ?
		where array_length(pgc.conkey,1)>1 and pgc.contype='u'
		and pgc.conrelid=c.table_name::regclass::oid
		order by position
	)
	select string_agg(column_name,',') as col, conname
	from comp group by conname;`
	_, err = tx.Query(&ukSchema, query, tableName)
	return
}

//GetIndex : Get index of table
func GetIndex(tx *pg.Tx, tableName string) (idx []model.Index, err error) {
	query := `
	with idx as (
		select
		--    t.relname as table_name
		    i.relname as index_name
		    , c.column_name
		    , am.amname
		    , array_position(ix.indkey::int[],c.ordinal_position::int) as position
		from
			pg_index ix
			join pg_class t on  t.oid = ix.indrelid
		    join pg_class i on i.oid = ix.indexrelid
		    JOIN pg_am am ON am.oid = i.relam
		    join unnest(ix.indkey::int[]) as colNo on true 
		    join information_schema.columns as c 
			on c.ordinal_position = colNo and c.table_name = t.relname
		where
		    t.relkind = 'r'
		    and ix.indisunique = false
		    and t.relname = ?
		   order by i.relname, position
	)
	select index_name 
	, string_agg(distinct amname,',') as itype
	, string_agg(column_name,',') as col
	from idx
	group by index_name;`
	_, err = tx.Query(&idx, query, tableName)
	return
}

//GetStructField will return struct fields
func GetStructField(model interface{}) (fields map[reflect.Value]reflect.StructField) {
	refObj := reflect.ValueOf(model)
	fields = make(map[reflect.Value]reflect.StructField)
	if refObj.Kind() == reflect.Ptr {
		refObj = refObj.Elem()
	}
	if refObj.IsValid() {
		for i := 0; i < refObj.NumField(); i++ {
			refField := refObj.Field(i)
			refType := refObj.Type().Field(i)
			if refType.Name[0] > 'Z' {
				continue
			}
			if refType.Anonymous && refField.Kind() == reflect.Struct {
				embdFields := GetStructField(refField.Interface())
				mergeMap(fields, embdFields)
			} else {
				if _, exists := refType.Tag.Lookup("sql"); exists == false {
					fmt.Println("No SQL tag in", refType.Name)
					panic("sql tag not fround")
				}
				fields[refField] = refType
			}
		}
	}
	return
}

func mergeMap(a, b map[reflect.Value]reflect.StructField) {
	for k, v := range b {
		a[k] = v
	}
}

//getSQLTag will return sql tag
func getSQLTag(refField reflect.StructField) (sqlTag string) {
	sqlTag = refField.Tag.Get("sql")
	sqlTag = strings.ToLower(sqlTag)
	return
}

//FieldType will return field type
func FieldType(refField reflect.StructField) (fType string) {
	sqlTag := getSQLTag(refField)
	vals := strings.Split(sqlTag, "type:")
	if len(vals) > 1 {
		fType = vals[1]
		fType = strings.Trim(strings.Split(fType, " ")[0], " ")
	}
	return
}

//RefTable will reutrn reference table
func RefTable(refField reflect.StructField) (refTable string) {
	sqlTag := getSQLTag(refField)
	refTag := strings.Split(sqlTag, "references")
	if len(refTag) > 1 {
		refTable = strings.Split(refTag[1], "(")[0]
		refTable = strings.Trim(refTable, " ")
	}
	return
}

//GetChoice will ask user choice
func GetChoice(sql string, skipPrompt bool) (choice string) {
	if skipPrompt {
		choice = Yes
	} else {
		fmt.Printf("%v\nWant to continue (y/n):", sql)
		fmt.Scan(&choice)
		choice = strings.ToLower(choice)
		if choice == Y {
			choice = Yes
		}
	}
	return
}

//SkipTag will check skiptag exists in model or not
func SkipTag(object interface{}) (flag bool) {
	refObj := reflect.ValueOf(object).Elem()
	if refObj.Kind() == reflect.Struct {
		if refObj.NumField() > 0 {
			if tag, exists := refObj.Type().Field(0).Tag.Lookup("history"); exists && tag == "skip" {
				flag = true
			}
		}
	}
	return
}

//GetHistoryTableName will reutrn history table name
func GetHistoryTableName(tableName string) string {
	return tableName + historyTag
}

//GetBeforeInsertTriggerName will return before insert trigger name
func GetBeforeInsertTriggerName(tableName string) string {
	return tableName + "_before_update"
}

//GetAfterInsertTriggerName will return after insert trigger name
func GetAfterInsertTriggerName(tableName string) string {
	return tableName + "_after_insert"
}

//GetAfterUpdateTriggerName will return after update trigger name
func GetAfterUpdateTriggerName(tableName string) string {
	return tableName + "_after_update"
}

//GetAfterDeleteTriggerName will return after delete trigger name
func GetAfterDeleteTriggerName(tableName string) string {
	return tableName + "_after_delete"
}

//IsAfterUpdateTriggerExists will check if after update triger exists
func IsAfterUpdateTriggerExists(tx *pg.Tx, tName string) (exists bool, err error) {
	var count int
	sql := `
	SELECT count(*) 
	FROM information_schema.triggers 
	WHERE event_object_table = ? 
	AND trigger_name = ?
	AND action_timing = 'AFTER'`
	afterUpdate := GetAfterUpdateTriggerName(tName)
	if _, err = tx.Query(&count, sql, tName, afterUpdate); err == nil && count > 0 {
		exists = true
	}
	return
}

//GetStrByLen will return string till given length
func GetStrByLen(str string, n int) string {
	if len(str) > n {
		str = string(str[:n-1])
	}
	return str
}
