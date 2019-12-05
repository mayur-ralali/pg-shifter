package shifter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/structs"
	"github.com/go-pg/pg"
	"github.com/mayur-tolexo/contour/adapter/psql"
	"github.com/mayur-tolexo/pg-shifter/model"
	"github.com/mayur-tolexo/pg-shifter/util"
)

//Alter Table
func (s *Shifter) alterTable(tx *pg.Tx, tableName string, skipPromt bool) (err error) {
	// initStructTableMap()
	var (
		columnSchema []model.ColSchema
		constraint   []model.ColSchema
		// uniqueKeySchema []model.UniqueKeySchema
	)
	_, isValid := s.table[tableName]

	if isValid == true {
		if columnSchema, err = util.GetColumnSchema(tx, tableName); err == nil {
			if constraint, err = util.GetConstraint(tx, tableName); err == nil {
				tSchema := MergeColumnConstraint(tableName, columnSchema, constraint)
				sSchema := s.GetStructSchema(tableName)

				if s.hisExists, err = util.IsAfterUpdateTriggerExists(tx, tableName); err == nil {
					err = s.compareSchema(tx, tSchema, sSchema, skipPromt)
				}
				// printSchema(tSchema, sSchema)
			}
		}
	} else {
		msg := "Invalid Table Name: " + tableName
		fmt.Println(msg)
		err = errors.New(msg)
	}
	return
}

//compareSchema will compare then table and struct column scheam and change accordingly
func (s *Shifter) compareSchema(tx *pg.Tx, tSchema, sSchema map[string]model.ColSchema,
	skipPromt bool) (err error) {

	var (
		added   bool
		removed bool
		modify  bool
	)

	defer func() { psql.LogMode(false) }()
	if s.Verbrose {
		psql.LogMode(true)
	}
	//adding column exists in struct but missing in db table
	if added, err = s.addRemoveCol(tx, sSchema, tSchema, Add, skipPromt); err == nil {
		//removing column exists in db table but missing in struct
		if removed, err = s.addRemoveCol(tx, tSchema, sSchema, Drop, skipPromt); err == nil {
			//TODO: modify column
			modify, err = s.modifyCol(tx, tSchema, sSchema, skipPromt)
		}
	}
	if err == nil && (added || removed || modify) {
		if err = s.createAlterStructLog(tSchema); err == nil {

			if added || removed {
				tName := getTableName(sSchema)
				err = s.createTrigger(tx, tName)
			}
		}
	}
	return
}

//addRemoveCol will add/drop missing column which exists in a but not in b
func (s *Shifter) addRemoveCol(tx *pg.Tx, a, b map[string]model.ColSchema,
	op string, skipPrompt bool) (isAlter bool, err error) {

	for col, schema := range a {
		var curIsAlter bool
		if v, exists := b[col]; exists == false {
			if curIsAlter, err = s.alterCol(tx, schema, op, skipPrompt); err != nil {
				break
			}
		} else if v.StructColumnName == "" {
			v.StructColumnName = schema.StructColumnName
			b[col] = v
		}
		isAlter = isAlter || curIsAlter
	}
	return
}

//alterCol will add/drop column in table
func (s *Shifter) alterCol(tx *pg.Tx, schema model.ColSchema,
	op string, skipPrompt bool) (isAlter bool, err error) {

	switch op {
	case Add:
		isAlter, err = s.addCol(tx, schema, skipPrompt)
	case Drop:
		isAlter, err = s.dropCol(tx, schema, skipPrompt)
	}
	return
}

//alterCol will add column in table
func (s *Shifter) addCol(tx *pg.Tx, schema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	dType := getAddColTypeSQL(schema)
	sql := getAddColSQL(schema.TableName, schema.ColumnName, dType)
	cSQL := getAddConstraintSQL(schema)

	if cSQL != "" {
		sql += "," + cSQL
	}

	//checking history table exists
	if s.hisExists {
		hName := util.GetHistoryTableName(schema.TableName)
		dType = getStructDataType(schema)
		sql += getAddColSQL(hName, schema.ColumnName, dType)
	}
	//history alter sql end

	if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
		err = getWrapError(schema.TableName, "add column", sql, err)
	}
	return
}

//getAddColSQL will return add column sql
func getAddColSQL(tName, cName, dType string) (sql string) {
	sql = fmt.Sprintf("ALTER TABLE %v ADD %v %v", tName, cName, dType)
	return
}

//dropCol will drop column from table
func (s *Shifter) dropCol(tx *pg.Tx, schema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	sql := getDropColSQL(schema.TableName, schema.ColumnName)
	//checking history table exists
	if s.hisExists {
		hName := util.GetHistoryTableName(schema.TableName)
		sql += ";\n" + getDropColSQL(hName, schema.ColumnName)
	}
	//history alter sql end

	if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
		err = getWrapError(schema.TableName, "drop column", sql, err)
	}
	return
}

//getAddColSQL will return add column sql
func getDropColSQL(tName, cName string) (sql string) {
	sql = fmt.Sprintf("ALTER TABLE %v DROP %v\n", tName, cName)
	return
}

//getFkName will return primary/unique/foreign key name
func getConstraintName(schema model.ColSchema) (keyName string) {
	var tag string
	switch schema.ConstraintType {
	case PrimaryKey:
		tag = "pkey"
	case Unique:
		tag = "key"
	case ForeignKey:
		tag = "fkey"
	}
	keyName = fmt.Sprintf("%v_%v_%v", schema.TableName, schema.ColumnName, tag)
	return
}

//getStructConstraintSQL will return constraint sql from scheam model
func getStructConstraintSQL(schema model.ColSchema) (sql string) {
	switch schema.ConstraintType {
	case PrimaryKey:
	case Unique:
	case ForeignKey:
		deleteTag := getConstraintTagByFlag(schema.DeleteType)
		updateTag := getConstraintTagByFlag(schema.UpdateType)
		sql = fmt.Sprintf(" REFERENCES %v(%v) ON DELETE %v ON UPDATE %v",
			schema.ForeignTableName, schema.ForeignColumnName, deleteTag, updateTag)
	}
	sql += getDefferSQL(schema)
	return
}

//getDefferSQL will reutrn deferable and initiall deferred sql
func getDefferSQL(schema model.ColSchema) (sql string) {
	if schema.IsDeferrable == Yes {
		sql = " DEFERRABLE"
	}
	if schema.InitiallyDeferred == Yes {
		sql += " INITIALLY DEFERRED"
	}
	return
}

//Get Constraint Tag by Flag
func getConstraintTagByFlag(flag string) (tag string) {
	switch flag {
	case "a":
		tag = "NO ACTION"
	case "r":
		tag = "RESTRICT"
	case "c":
		tag = "CASCADE"
	case "n":
		tag = "SET NULL"
	default:
		tag = "SET DEFAULT"
	}
	return
}

//getAddColTypeSQL will return add column type sql
func getAddColTypeSQL(schema model.ColSchema) (dType string) {
	dType = getStructDataType(schema)
	// dType += getUniqueDTypeSQL(schema.ConstraintType)
	dType += getNullDTypeSQL(schema.IsNullable)
	dType += getDefaultDTypeSQL(schema)
	return
}

//getStructDataType will return data type from schema
func getStructDataType(schema model.ColSchema) (dType string) {
	var exists bool

	if schema.SeqName != "" {
		dType = getSerialType(schema.SeqDataType)
	} else if schema.DataType == UserDefined {
		dType = schema.UdtName
	} else if dType, exists = rDataAlias[schema.DataType]; exists == false {
		dType = schema.DataType
	}
	if schema.CharMaxLen != "" {
		dType += "(" + schema.CharMaxLen + ")"
	}
	return
}

//getSerialType will return data type for serial
func getSerialType(seqDataType string) (dType string) {
	switch seqDataType {
	case "bigint":
		dType = "bigserial"
	case "smallint":
		dType = "smallserial"
	default:
		dType = "serial"
	}
	return
}

//getDefaultDTypeSQL will return default constraint string if exists in column
func getDefaultDTypeSQL(schema model.ColSchema) (str string) {

	if schema.ColumnDefault != "" && schema.SeqName == "" {
		str = " DEFAULT " + schema.ColumnDefault
	}
	return
}

//getNullDTypeSQL will return null/not null constraint string if exists in column
func getNullDTypeSQL(isNullable string) (str string) {
	if isNullable != "" {
		if isNullable == Yes {
			str = " NULL"
		} else {
			str = " NOT NULL"
		}
	}
	return
}

//getUniqueDTypeSQL will return unique constraint string if exists in column
func getUniqueDTypeSQL(schema model.ColSchema) (str string) {
	if schema.ConstraintType == Unique || schema.IsFkUnique {
		str = " " + Unique
	}
	return
}

//modifyCol will modify column of table by comparing with struct
func (s *Shifter) modifyCol(tx *pg.Tx, tSchema, sSchema map[string]model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	for col, tcSchema := range tSchema {
		var curIsAlter bool
		if scSchema, exists := sSchema[col]; exists {

			//modify data type
			if curIsAlter, err = s.modifyDataType(tx, tcSchema, scSchema, skipPrompt); err != nil {
				break
			}
			isAlter = isAlter || curIsAlter

			//if data type is not modified then only modify default type
			if curIsAlter == false {
				if curIsAlter, err = s.modifyDefault(tx, tcSchema, scSchema, skipPrompt); err != nil {
					break
				}
			}
			isAlter = isAlter || curIsAlter

			//modify not null constraint
			if curIsAlter, err = s.modifyNotNullConstraint(tx, tcSchema, scSchema, skipPrompt); err != nil {
				break
			}
			isAlter = isAlter || curIsAlter

			//modify pk/uk/fk constraint
			if curIsAlter, err = s.modifyConstraint(tx, tcSchema, scSchema, skipPrompt); err != nil {
				break
			}
			isAlter = isAlter || curIsAlter
		}
	}

	return
}

//modifyNotNullConstraint will modify not null by comparing table and structure
func (s *Shifter) modifyNotNullConstraint(tx *pg.Tx, tSchema, sSchema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	if tSchema.IsNullable != sSchema.IsNullable {
		option := Set
		if sSchema.IsNullable == Yes {
			option = Drop
		}
		sql := getNotNullColSQL(sSchema.TableName, sSchema.ColumnName, option)
		if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
			err = getWrapError(sSchema.TableName, "modify not null", sql, err)
		}
	}
	return
}

//getDropDefaultSQL will return set/drop not null constraint sql
func getNotNullColSQL(tName, cName, option string) (sql string) {
	sql = fmt.Sprintf("ALTER TABLE %v ALTER COLUMN %v %v NOT NULL",
		tName, cName, option)
	return
}

//modifyDataType will modify column data type by comparing with structure
func (s *Shifter) modifyDataType(tx *pg.Tx, tSchema, sSchema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	tDataType := getStructDataType(tSchema)
	sDataType := getStructDataType(sSchema)

	if tDataType != sDataType {
		//dropping default sql
		sql := getDropDefaultSQL(sSchema.TableName, sSchema.ColumnName)
		//modifying column type
		sql += getModifyColSQL(sSchema.TableName, sSchema.ColumnName, sDataType, sDataType)
		//adding back default sql
		sql += getSetDefaultSQL(sSchema.TableName, sSchema.ColumnName, sSchema.ColumnDefault)

		//checking history table exists
		if s.hisExists {
			hName := util.GetHistoryTableName(sSchema.TableName)
			sql += getModifyColSQL(hName, sSchema.ColumnName, sDataType, sDataType)
		}
		//history alter sql end

		if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
			err = getWrapError(sSchema.TableName, "modify datatype", sql, err)
		}
	}

	return
}

//getModifyColSQL will return modify column data type sql
func getModifyColSQL(tName, cName, dType, udtType string) (sql string) {

	sql = fmt.Sprintf("ALTER TABLE %v ALTER COLUMN %v TYPE %v USING (%v::%v);\n",
		tName, cName, dType, cName, udtType)
	return
}

//modifyDefault will modify default value by comparing table and structure
func (s *Shifter) modifyDefault(tx *pg.Tx, tSchema, sSchema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	tDefault := getTableDefault(tSchema, sSchema)
	sDefault := sSchema.ColumnDefault

	//for primary key default is series so should remove it
	if tSchema.ConstraintType != PrimaryKey &&
		tDefault != sDefault {
		sql := ""
		if sSchema.ColumnDefault == "" {
			sql = getDropDefaultSQL(sSchema.TableName, sSchema.ColumnName)
		} else {
			sql = getSetDefaultSQL(sSchema.TableName, sSchema.ColumnName, sSchema.ColumnDefault)
		}
		if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
			err = getWrapError(sSchema.TableName, "modify default", sql, err)
		}
	}
	return
}

//getTableDefault will return table default value based on null allowed
func getTableDefault(tSchema, sSchema model.ColSchema) (tDefault string) {
	tDefault = tSchema.ColumnDefault

	if tDefault == "" {
		if tSchema.IsNullable == Yes && sSchema.ColumnDefault != "" {
			tDefault = Null
		}
	} else if tSchema.ConstraintType != PrimaryKey &&
		tSchema.SeqName == "" && strings.Contains(tDefault, "::") {
		tDefault = strings.Split(tDefault, "::")[0]
	}
	tDefault = strings.ToLower(tDefault)

	return
}

//getDropDefaultSQL will return drop default constraint sql
func getDropDefaultSQL(tName, cName string) (sql string) {
	sql = fmt.Sprintf("ALTER TABLE %v ALTER COLUMN %v DROP DEFAULT;\n",
		tName, cName)
	return
}

//getSetDefaultSQL will return default column sql
func getSetDefaultSQL(tName, cName, dVal string) (sql string) {
	if dVal != "" {
		sql = fmt.Sprintf("ALTER TABLE %v ALTER COLUMN %v SET DEFAULT %v;\n",
			tName, cName, dVal)
	}
	return
}

//modifyConstraint will modify primary key/ unique key/ foreign key constraints by comparing table and structure
func (s *Shifter) modifyConstraint(tx *pg.Tx, tSchema, sSchema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {

	fmt.Println(sSchema.ColumnName, "T", tSchema.IsFkUnique, tSchema.ConstraintType, "S", sSchema.IsFkUnique, sSchema.ConstraintType)
	//if table and struct constraint doesn't match
	if tSchema.ConstraintType != sSchema.ConstraintType {
		if sSchema.ConstraintType == "" {
			isAlter, err = dropConstraint(tx, tSchema.TableName, tSchema.ConstraintName, skipPrompt)
		} else if tSchema.ConstraintType == "" {
			isAlter, err = addConstraint(tx, sSchema, skipPrompt)
		} else {

			// sql := ""

			// if tSchema.ConstraintType == ForeignKey && tSchema.IsFkUnique {
			// 	sql += getDropConstraintSQL(tSchema.TableName, tSchema.ConstraintName)
			// } else {
			// 	sql += getAlterAddConstraintSQL(sSchema)
			// }

			// if tSchema.ConstraintType == Unique && sSchema.IsFkUnique == false {
			// 	sql += getDropConstraintSQL(tSchema.TableName, tSchema.ConstraintName)
			// }

			fmt.Println(sql)
		}
	} else if tSchema.ConstraintType == ForeignKey {
		isAlter, err = modifyFkUniqueConstraint(tx, tSchema, sSchema, skipPrompt)
	}

	return
}

//modifyFkUniqueConstraint will modify unique key constraint
//if exists with foreign key on same column
func modifyFkUniqueConstraint(tx *pg.Tx, tSchema, sSchema model.ColSchema,
	skipPrompt bool) (isAlter bool, err error) {
	if tSchema.IsFkUnique != sSchema.IsFkUnique {
		if sSchema.IsFkUnique {
			sSchema.ConstraintType = Unique
			isAlter, err = addConstraint(tx, sSchema, skipPrompt)
		} else {
			isAlter, err = dropConstraint(tx, tSchema.TableName, tSchema.FkUniqueName, skipPrompt)
		}
	}
	return
}

//dropConstraint will drop constraint from table
func dropConstraint(tx *pg.Tx, tName, constraintName string,
	skipPrompt bool) (isAlter bool, err error) {
	sql := getDropConstraintSQL(tName, constraintName)
	if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
		err = getWrapError(tName, "drop constraint", sql, err)
	}
	return
}

//getDropConstraintSQL will return drop constraint sql
func getDropConstraintSQL(tName, constraintName string) (sql string) {
	return fmt.Sprintf("ALTER TABLE %v DROP CONSTRAINT %v;\n", tName, constraintName)
}

//addConstraint will add constraint on table column
func addConstraint(tx *pg.Tx, schema model.ColSchema, skipPrompt bool) (
	isAlter bool, err error) {

	sql := getAlterAddConstraintSQL(schema)
	if isAlter, err = execByChoice(tx, sql, skipPrompt); err != nil {
		err = getWrapError(schema.TableName, "add constraint", sql, err)
	}
	return
}

//getAlterAddConstraintSQL will return add constraint with alter table
func getAlterAddConstraintSQL(schema model.ColSchema) (sql string) {
	sql = getAddConstraintSQL(schema)
	sql = fmt.Sprintf("ALTER TABLE %v %v", schema.TableName, sql)
	return
}

//getAddConstraintSQL will return add constraint sql
func getAddConstraintSQL(schema model.ColSchema) (sql string) {
	if schema.ConstraintType != "" {
		sql = getStructConstraintSQL(schema)
		fkName := getConstraintName(schema)
		sql = fmt.Sprintf("ADD CONSTRAINT %v %v (%v) %v;\n",
			fkName, schema.ConstraintType, schema.ColumnName, sql)
	}
	return
}

func getAddConstraintSQL(schema)

//getWrapError will return wrapped error for better debugging
func getWrapError(tName, op string, sql string, err error) (werr error) {
	msg := fmt.Sprintf("%v %v error %v\nSQL: %v",
		tName, op, err.Error(), sql)
	werr = errors.New(msg)
	return
}

//execByChoice will execute by choice
func execByChoice(tx *pg.Tx, sql string, skipPrompt bool) (
	isAlter bool, err error) {

	choice := util.GetChoice(sql, skipPrompt)
	if choice == util.Yes {
		isAlter = true
		_, err = tx.Exec(sql)
	}
	return
}

//printSchema will print both schemas
func printSchema(tSchema, sSchema map[string]model.ColSchema) {
	for k, v1 := range tSchema {
		fmt.Println(k)
		if v2, exists := sSchema[k]; exists {
			tv := structs.Map(v1)
			sv := structs.Map(v2)
			for k, v := range tv {
				fmt.Println(k, v)
				fmt.Println(k, sv[k])
			}
			fmt.Println("---")
		}
	}
}

//MergeColumnConstraint : Merge Table Schema with Constraint
func MergeColumnConstraint(tName string, columnSchema,
	constraint []model.ColSchema) map[string]model.ColSchema {

	constraintMap := make(map[string]model.ColSchema)
	ColSchema := make(map[string]model.ColSchema)
	for _, curConstraint := range constraint {
		if v, exists := constraintMap[curConstraint.ColumnName]; exists {
			//if curent column is unique as foreign key as well
			if v.ConstraintType == Unique && curConstraint.ConstraintType == ForeignKey {
				v.FkUniqueName = v.ConstraintName
				v = curConstraint
				v.IsFkUnique = true
			} else if v.ConstraintType == ForeignKey && curConstraint.ConstraintType == Unique {
				v.FkUniqueName = curConstraint.ConstraintName
				v.IsFkUnique = true
			}
			constraintMap[curConstraint.ColumnName] = v
		} else {
			constraintMap[curConstraint.ColumnName] = curConstraint
		}
	}
	for _, curColumnSchema := range columnSchema {
		if curConstraint, exists :=
			constraintMap[curColumnSchema.ColumnName]; exists == true {
			curColumnSchema.ConstraintType = curConstraint.ConstraintType
			curColumnSchema.ConstraintName = curConstraint.ConstraintName
			curColumnSchema.IsDeferrable = curConstraint.IsDeferrable
			curColumnSchema.InitiallyDeferred = curConstraint.InitiallyDeferred
			curColumnSchema.ForeignTableName = curConstraint.ForeignTableName
			curColumnSchema.ForeignColumnName = curConstraint.ForeignColumnName
			curColumnSchema.UpdateType = curConstraint.UpdateType
			curColumnSchema.DeleteType = curConstraint.DeleteType
			curColumnSchema.IsFkUnique = curConstraint.IsFkUnique
			curColumnSchema.FkUniqueName = curConstraint.FkUniqueName
		}
		curColumnSchema.TableName = tName
		ColSchema[curColumnSchema.ColumnName] = curColumnSchema
	}
	return ColSchema
}
