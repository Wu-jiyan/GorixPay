package reporting

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"gorixpay/internal/transfer"
)

func TestNeutralizeCellDefusesFormulaPrefixes(t *testing.T) {
	cases := map[string]string{
		"=HYPERLINK(\"http://x\",\"点我\")": "'=HYPERLINK(\"http://x\",\"点我\")",
		"+SUM(1)":                         "'+SUM(1)",
		"-2+3":                            "'-2+3",
		"@SUM(A1)":                        "'@SUM(A1)",
		"\tcmd":                           "'\tcmd",
		"\rcmd":                           "'\rcmd",
		"招商银行":                            "招商银行",
		"Alice":                           "Alice",
		"":                                "",
		"张三 6222":                         "张三 6222",
	}
	for input, expected := range cases {
		if got := neutralizeCell(input); got != expected {
			t.Fatalf("neutralizeCell(%q)=%q want %q", input, got, expected)
		}
	}
}

func TestTransferExportNeutralizesMerchantControlledFields(t *testing.T) {
	fixture := newFixture(t, "reporting-injection")
	malicious := transfer.Order{
		ID: "transfer-evil", TransferNo: "P-EVIL", MerchantID: testMerchantID, OutTransferNo: "OUT-EVIL",
		Amount: 100, Currency: "CNY",
		AccountName: "=cmd|' /C calc'!A0", AccountNo: "+1+1", BankName: "-2+3", BranchName: "@SUM(A1)",
		Status: transfer.StatusPending, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := fixture.db.Create(&malicious).Error; err != nil {
		t.Fatal(err)
	}
	friendly := transfer.Order{
		ID: "transfer-ok", TransferNo: "P-OK", MerchantID: testMerchantID, OutTransferNo: "OUT-OK",
		Amount: 200, Currency: "CNY", AccountName: "招商银行", AccountNo: "6222", BankName: "招商银行",
		Status: transfer.StatusPending, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := fixture.db.Create(&friendly).Error; err != nil {
		t.Fatal(err)
	}
	response := fixture.get(t, "/api/v1/admin/export/transfers.csv", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	records := readCSV(t, response.Body.Bytes())
	rows := map[string][]string{}
	for _, record := range records[1:] {
		rows[record[0]] = record
	}
	evil, ok := rows["P-EVIL"]
	if !ok {
		t.Fatalf("missing malicious row: %v", rows)
	}
	if evil[5] != "'=cmd|' /C calc'!A0" || evil[6] != "'+1+1" || evil[7] != "'-2+3" || evil[8] != "'@SUM(A1)" {
		t.Fatalf("merchant fields were not neutralized: %v", evil)
	}
	okRow, found := rows["P-OK"]
	if !found {
		t.Fatalf("missing friendly row: %v", rows)
	}
	if okRow[5] != "招商银行" || okRow[7] != "招商银行" {
		t.Fatalf("plain Chinese name was altered: %v", okRow)
	}
	// The BOM and column order must stay intact.
	if len(records[0]) != len(transferHeader) || strings.Join(records[0], ",") != strings.Join(transferHeader, ",") {
		t.Fatalf("header changed: %v", records[0])
	}
}
