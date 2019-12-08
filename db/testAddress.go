package db

import "time"

//TestAddress Table structure as in DB
type TestAddress struct {
	tableName struct{}  `sql:"test_address"`
	AddressID int       `json:"address_id,omitempty" sql:"address_id,type:serial PRIMARY KEY"`
	City      string    `json:"city" sql:"city,type:varchar(25) UNIQUE"`
	Status    string    `json:"status,omitempty" sql:"status,type:varchar(200)"`
	CreatedBy int       `json:"created_by" sql:"created_by,type:int NOT NULL UNIQUE REFERENCES test_user(user_id)"`
	CreatedAt time.Time `json:"-" sql:"created_at,type:time NOT NULL DEFAULT NOW()"`
	UpdatedAt time.Time `json:"-" sql:"updated_at,type:timetz NOT NULL DEFAULT NOW()"`
}

//UniqueKey of the table. This is for composite unique keys
func (TestAddress) UniqueKey() []string {
	uk := []string{
		"address_id,status,city",
	}
	return uk
}
