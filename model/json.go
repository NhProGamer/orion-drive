package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// JSON is a json.RawMessage stored in a text/json column. The zero value
// marshals to SQL NULL and unmarshals from NULL to nil.
type JSON json.RawMessage

// GormDataType tells GORM which column type to use.
func (JSON) GormDataType() string { return "json" }

// Value implements driver.Valuer.
func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan implements sql.Scanner.
func (j *JSON) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		*j = append((*j)[0:0], v...)
	case string:
		*j = append((*j)[0:0], v...)
	default:
		return fmt.Errorf("model.JSON: unsupported scan type %T", value)
	}
	return nil
}

// MarshalJSON returns the raw JSON (or null).
func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON stores the raw bytes verbatim.
func (j *JSON) UnmarshalJSON(data []byte) error {
	if j == nil {
		return errors.New("model.JSON: UnmarshalJSON on nil pointer")
	}
	*j = append((*j)[0:0], data...)
	return nil
}

// Unmarshal decodes the stored JSON into v.
func (j JSON) Unmarshal(v any) error {
	if len(j) == 0 {
		return nil
	}
	return json.Unmarshal(j, v)
}

// MustJSON marshals v into a JSON value, panicking on error (encoding a
// plain map/struct never realistically fails).
func MustJSON(v any) JSON {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return JSON(b)
}
