package market

import (
	"fmt"
	"strings"
	"testing"
)

// FakeUSList is Nasdaq's list with n made-up stocks and the lines given,
// for tests here and in the app.
func fakeUSList(n int, lines ...string) []byte {
	var b strings.Builder
	b.WriteString("Nasdaq Traded|Symbol|Security Name|Listing Exchange|Market Category|ETF|Round Lot Size|Test Issue|Financial Status|CQS Symbol|NASDAQ Symbol|NextShares\n")
	for i := range n {
		fmt.Fprintf(&b, "Y|Z%c%c%c|Made Up %d Inc. Common Stock|N| |N|100|N||Z|Z|N\n", 'A'+i/676%26, 'A'+i/26%26, 'A'+i%26, i)
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("File Creation Time: 1029202604:01|||||||||||\n")
	return []byte(b.String())
}

// The list keeps stocks and ETFs, by Yahoo's codes and short names, and
// leaves out test issues, preferred shares, warrants, units and rights.
func TestParseUSList(t *testing.T) {
	list, err := ParseUSList(fakeUSList(1000,
		"Y|AAPL|Apple Inc. - Common Stock|Q|Q|N|100|N|N||AAPL|N",
		"Y|BRK.B|Berkshire Hathaway Inc. New Common Stock|N| |N|100|N||BRK.B|BRK.B|N",
		"Y|PDD|PDD Holdings Inc. - American Depositary Shares|Q|Q|N|100|N|N||PDD|N",
		"Y|QQQ|Invesco QQQ Trust, Series 1|Q|G|Y|100|N|N||QQQ|N",
		"Y|ZZTEST|Test Issue Common Stock|Q|Q|N|100|Y|N||ZZTEST|N",
		"Y|ABR$D|Arbor Realty Trust 6.375% Series D Preferred Stock|N| |N|100|N||ABRpD|ABR$D|N",
		"Y|AAC.W|Ares Acquisition Corporation III Redeemable warrants, each whole warrant exercisable for one Class A ordinary share at|N| |N|100|N||AAC.W|AAC.W|N",
		"Y|ACAQU|Acme Acquisition Corp - Units|Q|S|N|100|N|N||ACAQU|N",
		"Y|ACAQR|Acme Acquisition Corp - Rights|Q|S|N|100|N|N||ACAQR|N",
		"Y|UNH|UnitedHealth Group Incorporated Common Stock (DE)|N| |N|100|N||UNH|UNH|N",
	))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Instrument{}
	for _, in := range list {
		by[in.Code] = in
	}
	for code, name := range map[string]string{"AAPL": "Apple Inc.", "BRK-B": "Berkshire Hathaway Inc.", "PDD": "PDD Holdings Inc.", "QQQ": "Invesco QQQ Trust, Series 1", "UNH": "UnitedHealth Group Incorporated"} {
		if in := by[code]; in.Name != name || in.ID != "US:"+code || in.Ticker != code || in.Tick != 0.01 {
			t.Errorf("%s: %+v", code, in)
		}
	}
	for _, code := range []string{"ZZTEST", "ABR$D", "AAC.W", "AAC-W", "ACAQU", "ACAQR"} {
		if _, ok := by[code]; ok {
			t.Errorf("%s is in the list", code)
		}
	}
	if by["AAPL"].Alias != "苹果" || by["AAPL"].Rank != 3 || by["NVDA"].Rank != 1 || by["NVDA"].Alias != "英伟达" {
		t.Fatalf("popular: %+v %+v", by["AAPL"], by["NVDA"])
	}
	if len(list) != 1000+5+len(Popular)-5 { // the made up, the five kept, the popular but those five
		t.Fatalf("%d in the list", len(list))
	}
	for _, bad := range [][]byte{[]byte("<html>Access denied</html>"), fakeUSList(10)} {
		if _, err := ParseUSList(bad); err == nil {
			t.Errorf("%.30s… parsed", bad)
		}
	}
}
