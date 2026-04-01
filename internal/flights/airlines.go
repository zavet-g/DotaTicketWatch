package flights

var airlineNames = map[string]string{
	"SU": "Аэрофлот",
	"S7": "S7 Airlines",
	"MU": "China Eastern",
	"CZ": "China Southern",
	"CA": "Air China",
	"HU": "Hainan Airlines",
	"3U": "Sichuan Airlines",
	"FM": "Shanghai Airlines",
	"KN": "China United",
	"DP": "Победа",
	"UT": "UTair",
	"DV": "SCAT Airlines",
	"5F": "FlyOne",
}

func airlineName(code string) string {
	if code == "" {
		return "—"
	}
	if name, ok := airlineNames[code]; ok {
		return name
	}
	return code
}
