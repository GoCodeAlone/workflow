package module

import "testing"

func TestNormalizePlaceholders(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		driver string
		want   string
	}{
		{
			name:   "pgx passthrough",
			query:  "SELECT * FROM users WHERE id = $1",
			driver: "pgx",
			want:   "SELECT * FROM users WHERE id = $1",
		},
		{
			name:   "postgres passthrough",
			query:  "UPDATE users SET name = $1, email = $2 WHERE id = $3",
			driver: "postgres",
			want:   "UPDATE users SET name = $1, email = $2 WHERE id = $3",
		},
		{
			name:   "sqlite3 converts $N to ?",
			query:  "INSERT INTO users (name, email) VALUES ($1, $2)",
			driver: "sqlite3",
			want:   "INSERT INTO users (name, email) VALUES (?, ?)",
		},
		{
			name:   "sqlite converts $N to ?",
			query:  "UPDATE users SET name = $1 WHERE id = $2",
			driver: "sqlite",
			want:   "UPDATE users SET name = ? WHERE id = ?",
		},
		{
			name:   "sqlite3 multi placeholder",
			query:  "INSERT INTO items (a, b, c, d) VALUES ($1, $2, $3, $4)",
			driver: "sqlite3",
			want:   "INSERT INTO items (a, b, c, d) VALUES (?, ?, ?, ?)",
		},
		{
			name:   "no placeholders unchanged",
			query:  "SELECT * FROM users",
			driver: "sqlite3",
			want:   "SELECT * FROM users",
		},
		{
			name:   "already ? format for sqlite",
			query:  "SELECT * FROM users WHERE id = ?",
			driver: "sqlite3",
			want:   "SELECT * FROM users WHERE id = ?",
		},
		{
			name:   "empty driver treats as postgres",
			query:  "SELECT * FROM users WHERE id = $1",
			driver: "",
			want:   "SELECT * FROM users WHERE id = $1",
		},
		{
			name:   "unknown driver no modification",
			query:  "SELECT * FROM users WHERE id = $1",
			driver: "mysql",
			want:   "SELECT * FROM users WHERE id = $1",
		},
		{
			name:   "non-sequential placeholders unchanged",
			query:  "SELECT * FROM users WHERE id = $1 AND name = $3",
			driver: "sqlite3",
			want:   "SELECT * FROM users WHERE id = $1 AND name = $3",
		},
		{
			name:   "dollar in string literal preserved for postgres",
			query:  "SELECT * FROM users WHERE name = $1 AND bio LIKE '%$money%'",
			driver: "pgx",
			want:   "SELECT * FROM users WHERE name = $1 AND bio LIKE '%$money%'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizePlaceholders(tt.query, tt.driver)
			if got != tt.want {
				t.Errorf("normalizePlaceholders(%q, %q) = %q, want %q", tt.query, tt.driver, got, tt.want)
			}
		})
	}
}

func TestValidatePlaceholderCount(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		driver     string
		paramCount int
		wantErr    bool
	}{
		{"correct pg count", "SELECT * FROM users WHERE id = $1 AND name = $2", "pgx", 2, false},
		{"wrong pg count", "SELECT * FROM users WHERE id = $1", "pgx", 2, true},
		{"correct sqlite count", "SELECT * FROM users WHERE id = ? AND name = ?", "sqlite3", 2, false},
		{"wrong sqlite count", "SELECT * FROM users WHERE id = ?", "sqlite3", 2, true},
		{"no params no error", "SELECT * FROM users", "pgx", 0, false},
		{"unknown driver no validation", "SELECT * FROM users WHERE id = $1", "mysql", 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlaceholderCount(tt.query, tt.driver, tt.paramCount)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePlaceholderCount() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
