package shifter

import (
	"fmt"

	"github.com/go-pg/pg"
	"github.com/mayur-tolexo/flaw"
	"github.com/mayur-tolexo/pg-shifter/util"
)

//Create history table
func (s *Shifter) createHistory(tx *pg.Tx, tableName string) (err error) {
	if s.IsSkip(tableName) == false {
		historyTable := util.GetHistoryTableName(tableName)
		if tableExists := util.TableExists(tx, historyTable); tableExists == false {
			if err = s.execHistoryTable(tx, tableName, historyTable); err == nil {
				if err = s.dropHistoryConstraint(tx, historyTable); err == nil {
					err = s.createTrigger(tx, tableName)
				}
			}
		}
	}
	return
}

//dropHistory will drop history table
func (s *Shifter) dropHistory(tx *pg.Tx, tableName string, cascade bool) (err error) {
	historyTable := util.GetHistoryTableName(tableName)
	if tableExists := util.TableExists(tx, historyTable); tableExists == true {
		err = execTableDrop(tx, historyTable, cascade)
	}
	return
}

//dropHistoryConstraint will drop history table constraints
func (s *Shifter) dropHistoryConstraint(tx *pg.Tx, historyTable string) (err error) {
	if _, err = tx.Exec(fmt.Sprintf(`
	   	ALTER TABLE %v DROP COLUMN IF EXISTS updated_at;
		ALTER TABLE %v ADD COLUMN id BIGSERIAL PRIMARY KEY;
		ALTER TABLE %v ADD COLUMN action VARCHAR(20);`,
		historyTable, historyTable, historyTable)); err != nil {
		msg := fmt.Sprintf("Table: %v", historyTable)
		err = flaw.ExecError(err, msg)
		fmt.Println("History Constraint Error:", msg, err)
	}
	return
}

//execHistoryTable will execute history table creation
func (s *Shifter) execHistoryTable(tx *pg.Tx, tableName, historyTable string) (err error) {
	if _, err = tx.Exec(fmt.Sprintf("CREATE TABLE %v AS SELECT * FROM %v WHERE 1=2",
		historyTable, tableName)); err != nil {
		msg := fmt.Sprintf("Table: %v", tableName)
		err = flaw.ExecError(err, msg)
		fmt.Println("History Error:", msg, err)
	}
	return
}

//IsSkip will check table contain skip tags
func (s *Shifter) IsSkip(tableName string) (flag bool) {
	tableModel, isValid := s.table[tableName]
	if isValid {
		flag = util.SkipTag(tableModel)
	}
	return
}
