package model

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgreSQL migrations use a pgx execution-mode argument that must not shift SQL placeholders.
func TestPostgresSimpleProtocolBindVarNumbering(t *testing.T) {
	dialector, ok := postgres.New(postgres.Config{}).(*postgres.Dialector)
	require.True(t, ok)

	stmt := &gorm.Statement{
		Vars: []interface{}{pgx.QueryExecModeSimpleProtocol, 1},
	}
	var sql strings.Builder
	dialector.BindVarTo(&sql, stmt, 1)

	assert.Equal(t, "$1", sql.String())
}
