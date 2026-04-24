package db

import "github.com/jackc/pgx/v5/pgtype"

// Text creates a pgtype.Text from a string pointer.
func Text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// TextFromString creates a pgtype.Text from a string.
func TextFromString(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// TextValue returns the string value or empty string if null.
func TextValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// TextValid returns whether the pgtype.Text has a valid value.
func TextValid(t pgtype.Text) bool {
	return t.Valid
}
