package db

import (
	"context"
)

// ListUsers returns all users.
func (q *Queries) ListUsers(ctx context.Context) ([]User, error) {
	query := `SELECT id, name, display_name, real_name, is_bot, updated_at FROM users ORDER BY name`

	rows, err := q.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.DisplayName, &u.RealName, &u.IsBot, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	return users, rows.Err()
}
