// Read all Completed messages from civicrm driver's database and write them to 4priority service
// go build bb2prio.go ; strip bb2prio; cp bb2prio /media/sf_projects/bbpriority/

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/jmoiron/sqlx"
	_ "github.com/joho/godotenv/autoload"
	_ "github.com/pkg/errors"
)

// Read messages from database
type Contribution struct {
	ID                string          `db:"ID"`
	ORG               string          `db:"ORG"`
	CID               sql.NullString  `db:"CID"`
	QAMO_PARTNAME     sql.NullString  `db:"QAMO_PARTNAME"`
	QAMO_VAT          sql.NullString  `db:"QAMO_VAT"`
	QAMO_CUSTDES      sql.NullString  `db:"QAMO_CUSTDES"`
	QAMO_DETAILS      int64           `db:"QAMO_DETAILS"`
	QAMO_PARTDES      sql.NullString  `db:"QAMO_PARTDES"`
	QAMO_PAYMENTCODE  sql.NullString  `db:"QAMO_PAYMENTCODE"`
	QAMO_CARDNUM      sql.NullString  `db:"QAMO_CARDNUM"`
	QAMT_AUTHNUM      sql.NullString  `db:"QAMT_AUTHNUM"`
	QAMO_PAYMENTCOUNT sql.NullString  `db:"QAMO_PAYMENTCOUNT"`
	QAMO_VALIDMONTH   sql.NullString  `db:"QAMO_VALIDMONTH"`
	QAMO_PAYPRICE     float64         `db:"QAMO_PAYPRICE"`
	QAMO_CURRNCY      sql.NullString  `db:"QAMO_CURRNCY"`
	QAMO_PAYCODE      sql.NullInt64   `db:"QAMO_PAYCODE"`
	QAMO_FIRSTPAY     sql.NullFloat64 `db:"QAMO_FIRSTPAY"`
	QAMO_EMAIL        sql.NullString  `db:"QAMO_EMAIL"`
	QAMO_ADRESS       sql.NullString  `db:"QAMO_ADRESS"`
	QAMO_CITY         sql.NullString  `db:"QAMO_CITY"`
	QAMO_CELL         sql.NullString  `db:"QAMO_CELL"`
	QAMO_FROM         sql.NullString  `db:"QAMO_FROM"`
	QAMM_UDATE        sql.NullString  `db:"QAMM_UDATE"`
	QAMO_LANGUAGE     sql.NullString  `db:"QAMO_LANGUAGE"`
	QAMO_REFERENCE    sql.NullString  `db:"QAMO_REFERENCE"`
}

var (
	urlStr string
	err    error
)

func main() {

	host := os.Getenv("CIVI_HOST")
	if host == "" {
		host = "localhost"
	}
	dbName := os.Getenv("CIVI_DBNAME")
	if dbName == "" {
		dbName = "localhost"
	}
	user := os.Getenv("CIVI_USER")
	if user == "" {
		log.Fatalf("Unable to connect without username\n")
	}
	password := os.Getenv("CIVI_PASSWORD")
	if password == "" {
		log.Fatalf("Unable to connect without password\n")
	}
	protocol := os.Getenv("CIVI_PROTOCOL")
	if protocol == "" {
		log.Fatalf("Unable to connect without protocol\n")
	}
	startFromS := os.Getenv("CIVI_START_FROM")
	var startFrom int
	if startFromS == "" {
		startFrom = 38800
	} else {
		if startFrom, err = strconv.Atoi(startFromS); err != nil {
			fmt.Printf("Wrong value for Start From: (%s) %s\n", startFromS, err)
			startFrom = 38800
		}
	}
	urlStr = os.Getenv("PRIO_HOST")
	if urlStr == "" {
		log.Fatalf("Unable to connect Priority without its address\n")
	}

	db, stmt := OpenDb(host, user, password, protocol, dbName)
	defer closeDb(db)

	ReadMessages(db, stmt, startFrom)
}

// Connect to DB
func OpenDb(host string, user string, password string, protocol string, dbName string) (db *sqlx.DB, stmt *sql.Stmt) {

	dsn := fmt.Sprintf("%s:%s@%s(%s)/%s", user, password, protocol, host, dbName)
	if db, err = sqlx.Open("mysql", dsn); err != nil {
		log.Fatalf("DB connection error: %v\n", err)
	}
	if err = db.Ping(); err != nil {
		log.Fatalf("DB real connection error: %v\n", err)
	}

	if !isTableExists(db, dbName, "civicrm_bb_payment_responses") {
		log.Fatalf("Table 'civicrm_bb_payment_responses' does not exist\n")
	}

	stmt, err = db.Prepare("UPDATE civicrm_contribution SET invoice_number = 1 WHERE id = ?")
	if err != nil {
		log.Fatalf("Unable to prepare UPDATE statement: %v\n", err)
	}

	return
}

func closeDb(db *sqlx.DB) {
	_ = db.Close()
}

func isTableExists(db *sqlx.DB, dbName string, tableName string) (exists bool) {
	var name string

	if err = db.QueryRow(
		"SELECT table_name name FROM information_schema.tables WHERE table_schema = '" + dbName +
			"' AND table_name = '" + tableName + "' LIMIT 1").Scan(&name); err != nil {
		return false
	} else {
		return name == tableName
	}
}

func ReadMessages(db *sqlx.DB, markAsDone *sql.Stmt, startFrom int) {
	totalPaymentsRead := 0
	contribution := Contribution{}
	rows, err := db.Queryx(`
SELECT
  co.id ID,
  co.id QAMO_REFERENCE,
  con.nick_name ORG,
  fa.accounting_code QAMO_PARTNAME,
  fa.is_deductible QAMO_VAT,
  co.id CID,
  cc.display_name QAMO_CUSTDES,
  (
    SELECT count(1) + 1
    FROM civicrm_participant pa
    WHERE pa.registered_by_id = pp.participant_id
  ) QAMO_DETAILS,
  SUBSTRING(co.source, 1, 48) QAMO_PARTDES,
  CASE bb.cardtype
	WHEN 1 THEN 'ISR'
	WHEN 2 THEN 'CAL'
	WHEN 3 THEN 'DIN'
	WHEN 4 THEN 'AME'
	WHEN 6 THEN 'LEU'
	ELSE
    	CASE
    		WHEN co.trxn_id IS NULL THEN 'CAS'
        	WHEN co.trxn_id REGEXP '^[A-Z0-9]{17}$' THEN
            	CASE co.currency
                	WHEN 'USD' THEN 'PPU'
                    WHEN 'EUR' THEN 'PPE'
                    ELSE 'PPS'
                END
        	ELSE 'CAS'
    	END

  END QAMO_PAYMENTCODE,
  bb.token QAMO_CARDNUM,
  bb.approval QAMT_AUTHNUM,
  bb.cardnum QAMO_PAYMENTCOUNT,
  bb.cardexp QAMO_VALIDMONTH,
  COALESCE(bb.amount, co.total_amount) QAMO_PAYPRICE,
  CASE co.currency
    WHEN 'USD' THEN '$'
    WHEN 'EUR' THEN 'EUR'
    ELSE 'ש"ח'
  END QAMO_CURRNCY,
  COALESCE(bb.installments, 1) QAMO_PAYCODE,
  bb.firstpay QAMO_FIRSTPAY,
  (SELECT email FROM civicrm_email emails WHERE emails.contact_id = co.contact_id LIMIT 1) QAMO_EMAIL,
  (SELECT address.street_address FROM civicrm_address address WHERE address.contact_id = co.contact_id AND address.is_primary = 1 LIMIT 1) QAMO_ADRESS,
  (SELECT address.city FROM civicrm_address address WHERE address.contact_id = co.contact_id AND address.is_primary = 1 LIMIT 1) QAMO_CITY,
  (SELECT phone FROM civicrm_phone phones WHERE phones.contact_id = co.contact_id AND phones.is_primary = 1 LIMIT 1) QAMO_CELL,
  (SELECT country.name FROM civicrm_country country WHERE country.id = 
		(SELECT address.country_id FROM civicrm_address address WHERE address.contact_id = co.contact_id AND address.is_primary = 1 LIMIT 1) LIMIT 1) 
  			QAMO_FROM,
  COALESCE(bb.created_at, co.receive_date) QAMM_UDATE,
  CASE (SELECT country.name FROM civicrm_country country WHERE country.id = 
  					(SELECT address.country_id FROM civicrm_address address WHERE address.contact_id = co.contact_id AND address.is_primary = 1 LIMIT 1) LIMIT 1)
  			WHEN 'Israel' THEN 'HE' ELSE 'EN' END QAMO_LANGUAGE
FROM civicrm_contribution co
  INNER JOIN civicrm_contact cc ON co.contact_id = cc.id
  INNER JOIN civicrm_entity_financial_account efa ON co.financial_type_id = efa.entity_id AND efa.account_relationship = 1
  INNER JOIN civicrm_financial_account fa ON fa.id = efa.financial_account_id
  INNER JOIN civicrm_contact con ON con.id = fa.contact_id
  LEFT OUTER JOIN civicrm_bb_payment_responses bb ON bb.cid = co.id
  LEFT OUTER JOIN civicrm_participant_payment pp ON pp.contribution_id = co.id
WHERE
  co.id >= ?
  AND co.contribution_status_id = (
    SELECT value contributionStatus
    FROM civicrm_option_value
    WHERE option_group_id = (
      SELECT id contributionStatusID
      FROM civicrm_option_group
      WHERE name = "contribution_status"
      LIMIT 1
    ) AND name = 'Completed'
    LIMIT 1
  ) AND co.is_test = 0
  AND co.invoice_number IS NULL
  AND con.nick_name IN ('ben2', 'arvut2', 'meshp18')
	`, startFrom)
	if err != nil {
		log.Fatalf("Unable to select rows: %v\n", err)
	}

	for rows.Next() {
		// Read messages from DB
		err = rows.StructScan(&contribution)
		if err != nil {
			fmt.Printf("Table 'civicrm_contribution' access error: %v\n", err)
			continue
		}
		// Submit 2 priority
		submit2priority(contribution)

		// Update Reported2prio in case of success
		updateReported2prio(markAsDone, contribution.ID)
		totalPaymentsRead++
	}

	fmt.Printf("Total of %d payments were transferred to Priority\n", totalPaymentsRead)
}

//func timeIn(from string, name string) string {
//	loc, err := time.LoadLocation(name)
//	if err != nil {
//		return from;
//	}
//	t, err := time.Parse("2006-01-02 15:04:05", from)
//	if err != nil {
//		return from;
//	}
//	return t.In(loc).Format("2006-01-02 15:04:05")
//}

func submit2priority(contribution Contribution) {
	// priority's database structure
	type Priority struct {
		ID           string  `json:"id"`
		UserName     string  `json:"name"`
		Amount       float64 `json:"amount"`
		Currency     string  `json:"currency"`
		Email        string  `json:"email"`
		Phone        string  `json:"phone"`
		Address      string  `json:"address"`
		City         string  `json:"city"`
		Country      string  `json:"country"`
		Description  string  `json:"event"`
		Participants int64   `json:"participants"`
		Income       string  `json:"income"`
		Is46         bool    `json:"is46"`
		Token        string  `json:"token"`
		Approval     string  `json:"approval"`
		CardType     string  `json:"cardtype"`
		CardNum      string  `json:"cardnum"`
		CardExp      string  `json:"cardexp"`
		Installments int64   `json:"installments"`
		FirstPay     float64 `json:"firstpay"`
		CreatedAt    string  `json:"created_at"`
		Language     string  `json:"language"`
		Reference    string  `json:"reference"`
		Organization string  `json:"organization"`
		IsVisual     bool    `json:"is_visual"`
	}

	type Message struct {
		Error   bool
		Message string
	}

	priority := Priority{
		ID:           contribution.ID,
		UserName:     contribution.QAMO_CUSTDES.String,
		Participants: contribution.QAMO_DETAILS,
		Income:       contribution.QAMO_PARTNAME.String,
		Description:  contribution.QAMO_PARTDES.String,
		CardType:     contribution.QAMO_PAYMENTCODE.String,
		CardNum:      contribution.QAMO_PAYMENTCOUNT.String,
		CardExp:      contribution.QAMO_VALIDMONTH.String,
		Amount:       contribution.QAMO_PAYPRICE,
		Currency:     contribution.QAMO_CURRNCY.String,
		Installments: contribution.QAMO_PAYCODE.Int64,
		FirstPay:     contribution.QAMO_FIRSTPAY.Float64,
		Token:        contribution.QAMO_CARDNUM.String,
		Approval:     contribution.QAMT_AUTHNUM.String,
		Is46:         contribution.QAMO_VAT.String == "1",
		Email:        contribution.QAMO_EMAIL.String,
		Address:      contribution.QAMO_ADRESS.String,
		City:         contribution.QAMO_CITY.String,
		Country:      contribution.QAMO_FROM.String,
		Phone:        contribution.QAMO_CELL.String,
		CreatedAt:    contribution.QAMM_UDATE.String,
		Language:     contribution.QAMO_LANGUAGE.String,
		Reference:    contribution.QAMO_REFERENCE.String,
		Organization: contribution.ORG,
		IsVisual:     false, // CiviCRM produces Logical Hebrew
	}

	// convert QAMM_UDATE to IST
	// priority.CreatedAt = timeIn(priority.CreatedAt, "Asia/Jerusalem")

	marshal, err := json.Marshal(priority)
	if err != nil {
		fmt.Printf("Marshal error: %v\n", err)
		return
	}
	log.Printf("%s\n", marshal)

	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(marshal))
	if err != nil {
		fmt.Printf("NewRequest error: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("client.Do error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("ReadAll error: %v\n", err)
		return
	}
	message := Message{}
	if err := json.Unmarshal(body, &message); err != nil {
		fmt.Printf("Unmarshal error: %v\n", err)
		return
	}
	if message.Error {
		fmt.Printf("Response error: %s\n", message.Message)
		return
	}
}

func updateReported2prio(stmt *sql.Stmt, id string) {
	res, err := stmt.Exec(id)
	if err != nil {
		fmt.Printf("Update error: %v\n", err)
		return
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		fmt.Printf("Update error: %v\n", err)
		return
	}
	if rowCnt != 1 {
		fmt.Printf("Update error: %d rows were updated instead of 1\n", rowCnt)
		return
	}
}
