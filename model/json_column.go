package model

import (
	"database/sql/driver"
	"fmt"
)

// jsonColumnValue returns a PostgreSQL-safe bind value for json/jsonb columns.
// pgx PreferSimpleProtocol encodes []byte as hex (\x...), which PostgreSQL then
// rejects with SQLSTATE 22P02. A JSON text string is valid for all supported DBs.
func jsonColumnValue(payload []byte, err error) (driver.Value, error) {
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, nil
	}
	return string(payload), nil
}

func scanJSONColumn(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []byte:
		if len(v) == 0 {
			return nil, nil
		}
		out := make([]byte, len(v))
		copy(out, v)
		return out, nil
	case string:
		if v == "" {
			return nil, nil
		}
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("unsupported JSON column type %T", value)
	}
}
