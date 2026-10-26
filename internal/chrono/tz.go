package chrono

import "github.com/costaluu/taskthing/internal/js"

// timezone.js: abbreviation → offset in minutes, or a DST-aware rule.

type dstRule struct {
	during, non int
	start, end  func(year int) js.Date
}

var timezoneAbbrMap = map[string]int{
	"ACDT": 630, "ACST": 570, "ADT": -180, "AEDT": 660, "AEST": 600, "AFT": 270, "AKDT": -480,
	"AKST": -540, "ALMT": 360, "AMST": -180, "AMT": -240, "ANAST": 720, "ANAT": 720, "AQTT": 300,
	"ART": -180, "AST": -240, "AWDT": 540, "AWST": 480, "AZOST": 0, "AZOT": -60, "AZST": 300,
	"AZT": 240, "BNT": 480, "BOT": -240, "BRST": -120, "BRT": -180, "BST": 60, "BTT": 360,
	"CAST": 480, "CAT": 120, "CCT": 390, "CDT": -300, "CEST": 120, "CHADT": 825, "CHAST": 765,
	"CKT": -600, "CLST": -180, "CLT": -240, "COT": -300, "CST": -360, "CVT": -60, "CXT": 420,
	"ChST": 600, "DAVT": 420, "EASST": -300, "EAST": -360, "EAT": 180, "ECT": -300, "EDT": -240,
	"EEST": 180, "EET": 120, "EGST": 0, "EGT": -60, "EST": -300, "FJST": 780, "FJT": 720,
	"FKST": -180, "FKT": -240, "FNT": -120, "GALT": -360, "GAMT": -540, "GET": 240, "GFT": -180,
	"GILT": 720, "GMT": 0, "GST": 240, "GYT": -240, "HAA": -180, "HAC": -300, "HADT": -540,
	"HAE": -240, "HAP": -420, "HAR": -360, "HAST": -600, "HAT": -90, "HAY": -480, "HKT": 480,
	"HLV": -210, "HNA": -240, "HNC": -360, "HNE": -300, "HNP": -480, "HNR": -420, "HNT": -150,
	"HNY": -540, "HOVT": 420, "ICT": 420, "IDT": 180, "IOT": 360, "IRDT": 270, "IRKST": 540,
	"IRKT": 540, "IRST": 210, "IST": 330, "JST": 540, "KGT": 360, "KRAST": 480, "KRAT": 480,
	"KST": 540, "KUYT": 240, "LHDT": 660, "LHST": 630, "LINT": 840, "MAGST": 720, "MAGT": 720,
	"MART": -510, "MAWT": 300, "MDT": -360, "MESZ": 120, "MEZ": 60, "MHT": 720, "MMT": 390,
	"MSD": 240, "MSK": 180, "MST": -420, "MUT": 240, "MVT": 300, "MYT": 480, "NCT": 660,
	"NDT": -90, "NFT": 690, "NOVST": 420, "NOVT": 360, "NPT": 345, "NST": -150, "NUT": -660,
	"NZDT": 780, "NZST": 720, "OMSST": 420, "OMST": 420, "PDT": -420, "PET": -300, "PETST": 720,
	"PETT": 720, "PGT": 600, "PHOT": 780, "PHT": 480, "PKT": 300, "PMDT": -120, "PMST": -180,
	"PONT": 660, "PST": -480, "PWT": 540, "PYST": -180, "PYT": -240, "RET": 240, "SAMT": 240,
	"SAST": 120, "SBT": 660, "SCT": 240, "SGT": 480, "SRT": -180, "SST": -660, "TAHT": -600,
	"TFT": 300, "TJT": 300, "TKT": 780, "TLT": 540, "TMT": 300, "TVT": 720, "ULAT": 480, "UTC": 0,
	"UYST": -120, "UYT": -180, "UZT": 300, "VET": -210, "VLAST": 660, "VLAT": 660, "VUT": 660,
	"WAST": 120, "WAT": 60, "WEST": 60, "WESZ": 60, "WET": 0, "WEZ": 0, "WFT": 720, "WGST": -120,
	"WGT": -180, "WIB": 420, "WIT": 540, "WITA": 480, "WST": 780, "WT": 0, "YAKST": 600,
	"YAKT": 600, "YAPT": 600, "YEKST": 360, "YEKT": 360,
}

func usRule(during, non int) dstRule {
	return dstRule{
		during: during, non: non,
		start: func(y int) js.Date { return getNthWeekdayOfMonth(y, 3, 0, 2, 2) },
		end:   func(y int) js.Date { return getNthWeekdayOfMonth(y, 11, 0, 1, 2) },
	}
}

var timezoneDstMap = map[string]dstRule{
	"CET": {during: 120, non: 60,
		start: func(y int) js.Date { return getLastWeekdayOfMonth(y, 3, 0, 2) },
		end:   func(y int) js.Date { return getLastWeekdayOfMonth(y, 10, 0, 3) }},
	"CT": usRule(-300, -360),
	"ET": usRule(-240, -300),
	"MT": usRule(-360, -420),
	"PT": usRule(-420, -480),
}

func toTimezoneOffset(abbr string, date js.Date) (int, bool) {
	if v, ok := timezoneAbbrMap[abbr]; ok {
		return v, true
	}
	rule, ok := timezoneDstMap[abbr]
	if !ok {
		return 0, false
	}
	year := date.GetFullYear()
	if date.Time() > rule.start(year).Time() && !(date.Time() > rule.end(year).Time()) {
		return rule.during, true
	}
	return rule.non, true
}

func getNthWeekdayOfMonth(year, month, weekday, n, hour int) js.Date {
	day := 0
	for i := 0; i < n; {
		day++
		if js.NewLocal(year, month-1, day, 0, 0, 0, 0).GetDay() == weekday {
			i++
		}
	}
	return js.NewLocal(year, month-1, day, hour, 0, 0, 0)
}

func getLastWeekdayOfMonth(year, month, weekday, hour int) js.Date {
	oneIndexed := weekday
	if weekday == 0 {
		oneIndexed = 7
	}
	date := js.NewLocal(year, month, 1, 12, 0, 0, 0)
	first := date.GetDay()
	if first == 0 {
		first = 7
	}
	var diff int
	switch {
	case first == oneIndexed:
		diff = 7
	case first < oneIndexed:
		diff = 7 + first - oneIndexed
	default:
		diff = first - oneIndexed
	}
	date = date.SetDate(date.GetDate() - diff)
	return js.NewLocal(year, month-1, date.GetDate(), hour, 0, 0, 0)
}
