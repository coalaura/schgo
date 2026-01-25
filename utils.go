package schgo

import (
	"strconv"
	"strings"
)

func areNumericallyEqual(a, b string) bool {
	if a == b {
		return true
	}

	valA, _ := unquoteLiteral(a)
	valB, _ := unquoteLiteral(b)

	fA, errA := strconv.ParseFloat(valA, 64)
	fB, errB := strconv.ParseFloat(valB, 64)

	if errA != nil || errB != nil {
		return false
	}

	return fA == fB
}

func unquoteLiteral(s string) (string, bool) {
	s = strings.TrimSpace(s)

	if len(s) < 2 {
		return s, false
	}

	q := s[0]

	if (q != '\'' && q != '"') || s[len(s)-1] != q {
		return s, false
	}

	inner := s[1 : len(s)-1]

	if q == '\'' {
		return strings.ReplaceAll(inner, "''", "'"), true
	}

	return strings.ReplaceAll(inner, `""`, `"`), true
}

func isNumericCol(typ string) bool {
	u := strings.ToUpper(strings.TrimSpace(typ))

	if idx := strings.IndexByte(u, '('); idx != -1 {
		u = u[:idx]
	}

	u = strings.Fields(u)[0]

	switch u {
	case "INT", "INTEGER", "BIGINT", "SMALLINT", "TINYINT", "MEDIUMINT",
		"FLOAT", "DOUBLE", "REAL", "DECIMAL", "NUMERIC",
		"SERIAL", "BIGSERIAL", "SMALLSERIAL":
		return true
	}

	if u == "DOUBLE" && strings.Contains(strings.ToUpper(typ), "PRECISION") {
		return true
	}

	return false
}

func defaultsMatch(col *Column, existing *ColumnInfo) bool {
	if !existing.Default.Valid {
		return col.Def == ""
	}

	if col.Def == existing.Default.String {
		return true
	}

	if isNumericCol(col.Type) {
		return areNumericallyEqual(col.Def, existing.Default.String)
	}

	defVal, _ := unquoteLiteral(col.Def)
	existVal, _ := unquoteLiteral(existing.Default.String)

	return defVal == existVal
}
