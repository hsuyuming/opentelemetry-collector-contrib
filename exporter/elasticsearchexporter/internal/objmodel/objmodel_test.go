// Copyright 2021, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package objmodel

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/elastic/go-structform/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/pdata"
)

var dijkstra = time.Date(1930, 5, 11, 16, 33, 11, 123456789, time.UTC)

func TestObjectModel_CreateMap(t *testing.T) {
	tests := map[string]struct {
		build func() Document
		want  Document
	}{
		"from empty map": {
			build: func() Document {
				return DocumentFromAttributes(pdata.NewAttributeMap().InitFromMap(map[string]pdata.AttributeValue{}))
			},
		},
		"from map": {
			build: func() Document {
				return DocumentFromAttributes(pdata.NewAttributeMap().InitFromMap(map[string]pdata.AttributeValue{
					"i":   pdata.NewAttributeValueInt(42),
					"str": pdata.NewAttributeValueString("test"),
				}))
			},
			want: Document{[]field{{"i", IntValue(42)}, {"str", StringValue("test")}}},
		},
		"ignores nil values": {
			build: func() Document {
				return DocumentFromAttributes(pdata.NewAttributeMap().InitFromMap(map[string]pdata.AttributeValue{
					"null": pdata.NewAttributeValueNull(),
					"str":  pdata.NewAttributeValueString("test"),
				}))
			},
			want: Document{[]field{{"str", StringValue("test")}}},
		},
		"from map with prefix": {
			build: func() Document {
				return DocumentFromAttributesWithPath("prefix", pdata.NewAttributeMap().InitFromMap(map[string]pdata.AttributeValue{
					"i":   pdata.NewAttributeValueInt(42),
					"str": pdata.NewAttributeValueString("test"),
				}))
			},
			want: Document{[]field{{"prefix.i", IntValue(42)}, {"prefix.str", StringValue("test")}}},
		},
		"add attributes with key": {
			build: func() (doc Document) {
				doc.AddAttributes("prefix", pdata.NewAttributeMap().InitFromMap(map[string]pdata.AttributeValue{
					"i":   pdata.NewAttributeValueInt(42),
					"str": pdata.NewAttributeValueString("test"),
				}))
				return doc
			},
			want: Document{[]field{{"prefix.i", IntValue(42)}, {"prefix.str", StringValue("test")}}},
		},
		"add attribute flattens a map value": {
			build: func() (doc Document) {
				mapVal := pdata.NewAttributeValueMap()
				m := mapVal.MapVal()
				m.InsertInt("i", 42)
				m.InsertString("str", "test")
				doc.AddAttribute("prefix", mapVal)
				return doc
			},
			want: Document{[]field{{"prefix.i", IntValue(42)}, {"prefix.str", StringValue("test")}}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			doc := test.build()
			doc.Sort()
			assert.Equal(t, test.want, doc)
		})
	}
}

func TestDocument_Sort(t *testing.T) {
	tests := map[string]struct {
		build func() Document
		want  Document
	}{
		"keys are sorted": {
			build: func() (doc Document) {
				doc.AddInt("z", 26)
				doc.AddInt("a", 1)
				return doc
			},
			want: Document{[]field{{"a", IntValue(1)}, {"z", IntValue(26)}}},
		},
		"sorting is stable": {
			build: func() (doc Document) {
				doc.AddInt("a", 1)
				doc.AddInt("c", 3)
				doc.AddInt("a", 2)
				return doc
			},
			want: Document{[]field{{"a", IntValue(1)}, {"a", IntValue(2)}, {"c", IntValue(3)}}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			doc := test.build()
			doc.Sort()
			assert.Equal(t, test.want, doc)
		})
	}

}

func TestObjectModel_Dedup(t *testing.T) {
	tests := map[string]struct {
		build func() Document
		want  Document
	}{
		"no duplicates": {
			build: func() (doc Document) {
				doc.AddInt("a", 1)
				doc.AddInt("c", 3)
				return doc
			},
			want: Document{[]field{{"a", IntValue(1)}, {"c", IntValue(3)}}},
		},
		"duplicate keys": {
			build: func() (doc Document) {
				doc.AddInt("a", 1)
				doc.AddInt("c", 3)
				doc.AddInt("a", 2)
				return doc
			},
			want: Document{[]field{{"a", ignoreValue}, {"a", IntValue(2)}, {"c", IntValue(3)}}},
		},
		"duplicate after flattening from map: namespace object at end": {
			build: func() Document {
				namespace := pdata.NewAttributeValueMap()
				namespace.MapVal().InsertInt("a", 23)

				am := pdata.NewAttributeMap()
				am.InsertInt("namespace.a", 42)
				am.InsertString("toplevel", "test")
				am.Insert("namespace", namespace)
				return DocumentFromAttributes(am)
			},
			want: Document{[]field{{"namespace.a", ignoreValue}, {"namespace.a", IntValue(23)}, {"toplevel", StringValue("test")}}},
		},
		"duplicate after flattening from map: namespace object at beginning": {
			build: func() Document {
				namespace := pdata.NewAttributeValueMap()
				namespace.MapVal().InsertInt("a", 23)

				am := pdata.NewAttributeMap()
				am.Insert("namespace", namespace)
				am.InsertInt("namespace.a", 42)
				am.InsertString("toplevel", "test")
				return DocumentFromAttributes(am)
			},
			want: Document{[]field{{"namespace.a", ignoreValue}, {"namespace.a", IntValue(42)}, {"toplevel", StringValue("test")}}},
		},
		"dedup in arrays": {
			build: func() (doc Document) {
				var embedded Document
				embedded.AddInt("a", 1)
				embedded.AddInt("c", 3)
				embedded.AddInt("a", 2)

				doc.Add("arr", ArrValue(Value{kind: KindObject, doc: embedded}))
				return doc
			},
			want: Document{[]field{{"arr", ArrValue(Value{kind: KindObject, doc: Document{[]field{
				{"a", ignoreValue},
				{"a", IntValue(2)},
				{"c", IntValue(3)},
			}}})}}},
		},
		"dedup mix of primitive and object lifts primitive": {
			build: func() (doc Document) {
				doc.AddInt("namespace", 1)
				doc.AddInt("namespace.a", 2)
				return doc
			},
			want: Document{[]field{{"namespace.a", IntValue(2)}, {"namespace.value", IntValue(1)}}},
		},
		"dedup removes primitive if value exists": {
			build: func() (doc Document) {
				doc.AddInt("namespace", 1)
				doc.AddInt("namespace.a", 2)
				doc.AddInt("namespace.value", 3)
				return doc
			},
			want: Document{[]field{{"namespace.a", IntValue(2)}, {"namespace.value", ignoreValue}, {"namespace.value", IntValue(3)}}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			doc := test.build()
			doc.Sort()
			doc.Dedup()
			assert.Equal(t, test.want, doc)
		})
	}
}

func TestValue_FromAttribute(t *testing.T) {
	tests := map[string]struct {
		in   pdata.AttributeValue
		want Value
	}{
		"null": {
			in:   pdata.NewAttributeValueNull(),
			want: nilValue,
		},
		"string": {
			in:   pdata.NewAttributeValueString("test"),
			want: StringValue("test"),
		},
		"int": {
			in:   pdata.NewAttributeValueInt(23),
			want: IntValue(23),
		},
		"double": {
			in:   pdata.NewAttributeValueDouble(3.14),
			want: DoubleValue(3.14),
		},
		"bool": {
			in:   pdata.NewAttributeValueBool(true),
			want: BoolValue(true),
		},
		"empty array": {
			in:   pdata.NewAttributeValueArray(),
			want: Value{kind: KindArr},
		},
		"non-empty array": {
			in: func() pdata.AttributeValue {
				v := pdata.NewAttributeValueArray()
				arr := v.ArrayVal()
				arr.Append(pdata.NewAttributeValueInt(1))
				return v
			}(),
			want: ArrValue(IntValue(1)),
		},
		"empty map": {
			in:   pdata.NewAttributeValueMap(),
			want: Value{kind: KindObject},
		},
		"non-empty map": {
			in: func() pdata.AttributeValue {
				v := pdata.NewAttributeValueMap()
				m := v.MapVal()
				m.Insert("a", pdata.NewAttributeValueInt(1))
				return v
			}(),
			want: Value{kind: KindObject, doc: Document{[]field{{"a", IntValue(1)}}}},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			v := ValueFromAttribute(test.in)
			assert.Equal(t, test.want, v)
		})
	}
}

func TestValue_Serialize(t *testing.T) {
	tests := map[string]struct {
		value Value
		want  string
	}{
		"nil value":         {value: nilValue, want: "null"},
		"bool value: true":  {value: BoolValue(true), want: "true"},
		"bool value: false": {value: BoolValue(false), want: "false"},
		"int value":         {value: IntValue(42), want: "42"},
		"double value":      {value: DoubleValue(3.14), want: "3.14"},
		"NaN is undefined":  {value: DoubleValue(math.NaN()), want: "null"},
		"Inf is undefined":  {value: DoubleValue(math.Inf(0)), want: "null"},
		"string value":      {value: StringValue("Hello World!"), want: `"Hello World!"`},
		"timestamp": {
			value: TimestampValue(dijkstra),
			want:  `"1930-05-11T16:33:11.123456789Z"`,
		},
		"array": {
			value: ArrValue(BoolValue(true), IntValue(23)),
			want:  `[true,23]`,
		},
		"object": {
			value: func() Value {
				doc := Document{}
				doc.AddString("a", "b")
				return Value{kind: KindObject, doc: doc}
			}(),
			want: `{"a":"b"}`,
		},
		"empty object": {
			value: Value{kind: KindObject, doc: Document{}},
			want:  "null",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var buf strings.Builder
			err := test.value.iterJSON(json.NewVisitor(&buf), false)
			require.NoError(t, err)
			assert.Equal(t, test.want, buf.String())
		})
	}
}
