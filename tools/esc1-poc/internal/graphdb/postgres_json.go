/*
Copyright 2026 Specter Ops, Inc.

Licensed under the Apache License, Version 2.0
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/
package graphdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/specterops/dawgs/drivers/pg"
)

// The PoC carries database identifiers through JSON batches. Preserve integer
// tokens exactly without changing the shared DAWGS driver's numeric behavior.
func postgresAfterConnect(ctx context.Context, connection *pgx.Conn) error {
	connection.TypeMap().RegisterType(&pgtype.Type{Name: "json", OID: pgtype.JSONOID, Codec: &pgtype.JSONCodec{Marshal: json.Marshal, Unmarshal: decodePostgresJSON}})
	connection.TypeMap().RegisterType(&pgtype.Type{Name: "jsonb", OID: pgtype.JSONBOID, Codec: &pgtype.JSONBCodec{Marshal: json.Marshal, Unmarshal: decodePostgresJSON}})
	return pg.AfterPooledConnectionEstablished(ctx, connection)
}

func decodePostgresJSON(data []byte, destination any) error {
	var (
		decoder  = json.NewDecoder(bytes.NewReader(data))
		decoded  any
		trailing any
	)
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("JSON answer contains trailing data")
	}
	converted, err := exactPostgresJSONNumbers(decoded)
	if err != nil {
		return err
	}
	target := reflect.ValueOf(destination)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("JSON answer requires a nonnil destination pointer")
	}
	if converted == nil {
		target.Elem().SetZero()
		return nil
	}
	value := reflect.ValueOf(converted)
	if value.Type().AssignableTo(target.Elem().Type()) {
		target.Elem().Set(value)
		return nil
	}
	// pgx's untyped result values take the exact path above. Retain ordinary JSON
	// decoding for explicitly typed scan destinations.
	return json.Unmarshal(data, destination)
}

func exactPostgresJSONNumbers(value any) (any, error) {
	switch value := value.(type) {
	case json.Number:
		if !strings.ContainsAny(value.String(), ".eE") {
			integer, err := value.Int64()
			if err != nil {
				return nil, fmt.Errorf("JSON integer answer is outside the signed 64-bit range")
			}
			return integer, nil
		}
		number, err := strconv.ParseFloat(value.String(), 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return nil, fmt.Errorf("JSON decimal answer is outside the finite float64 range")
		}
		return number, nil
	case map[string]any:
		for key, item := range value {
			converted, err := exactPostgresJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			value[key] = converted
		}
		return value, nil
	case []any:
		for index, item := range value {
			converted, err := exactPostgresJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			value[index] = converted
		}
		return value, nil
	default:
		return value, nil
	}
}
