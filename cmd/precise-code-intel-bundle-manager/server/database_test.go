package server

import (
	"reflect"
	"testing"

	"github.com/sourcegraph/sourcegraph/internal/sqliteutil"
)

func init() {
	sqliteutil.SetLocalLibpath()
	sqliteutil.MustRegisterSqlite3WithPcre()
}

func TestDatabaseExists(t *testing.T) {
	testCases := []struct {
		path     string
		expected bool
	}{
		{"cmd/lsif-go/main.go", true},
		{"internal/index/indexer.go", true},
		{"missing.go", false},
	}

	withTestDatabase(t, func(db *Database) {
		for _, testCase := range testCases {
			if exists, err := db.Exists(testCase.path); err != nil {
				t.Errorf("unexpected error %s", err)
			} else if exists != testCase.expected {
				t.Errorf("unexpected exists result for %s. want=%v have=%v", testCase.path, testCase.expected, exists)
			}
		}
	})
}

func TestDatabaseDefinitions(t *testing.T) {
	// `\ts, err := indexer.Index()` -> `\t Index() (*Stats, error)`
	//                      ^^^^^           ^^^^^

	withTestDatabase(t, func(db *Database) {
		if actual, err := db.Definitions("cmd/lsif-go/main.go", 110, 22); err != nil {
			t.Errorf("unexpected error %s", err)
		} else {
			expected := []InternalLocation{
				{
					Path:  "internal/index/indexer.go",
					Range: newRange(20, 1, 20, 6),
				},
			}

			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("unexpected definitions locations. want=%v have=%v", expected, actual)
			}
		}
	})
}

func TestDatabaseReferences(t *testing.T) {
	// `func (w *Writer) EmitRange(start, end Pos) (string, error) {`
	//                   ^^^^^^^^^
	//
	// -> `\t\trangeID, err := i.w.EmitRange(lspRange(ipos, ident.Name, isQuotedPkgName))`
	//                             ^^^^^^^^^
	//
	// -> `\t\t\trangeID, err = i.w.EmitRange(lspRange(ipos, ident.Name, false))`
	//                              ^^^^^^^^^

	withTestDatabase(t, func(db *Database) {
		if actual, err := db.References("protocol/writer.go", 85, 20); err != nil {
			t.Errorf("unexpected error %s", err)
		} else {
			expected := []InternalLocation{
				{
					Path:  "protocol/writer.go",
					Range: newRange(85, 17, 85, 26),
				}, {
					Path:  "internal/index/indexer.go",
					Range: newRange(529, 22, 529, 31),
				}, {
					Path:  "internal/index/indexer.go",
					Range: newRange(380, 22, 380, 31),
				},
			}

			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("unexpected reference locations. want=%v have=%v", expected, actual)
			}
		}
	})
}

func TestDatabaseHover(t *testing.T) {
	// `\tcontents, err := findContents(pkgs, p, f, obj)`
	//                     ^^^^^^^^^^^^

	withTestDatabase(t, func(db *Database) {
		if actualText, actualRange, exists, err := db.Hover("internal/index/indexer.go", 628, 20); err != nil {
			t.Errorf("unexpected error %s", err)
		} else if !exists {
			t.Errorf("no hover found")
		} else {
			docstring := "findContents returns contents used as hover info for given object."
			signature := "func findContents(pkgs []*Package, p *Package, f *File, obj Object) ([]MarkedString, error)"
			expectedText := "```go\n" + signature + "\n```\n\n---\n\n" + docstring
			expectedRange := newRange(628, 18, 628, 30)

			if !reflect.DeepEqual(actualText, expectedText) {
				t.Errorf("unexpected hover text. want=%v have=%v", expectedText, actualText)
			}

			if !reflect.DeepEqual(actualRange, expectedRange) {
				t.Errorf("unexpected hover range. want=%v have=%v", expectedRange, actualRange)
			}
		}
	})
}

func TestDatabaseMonikersByPosition(t *testing.T) {
	// `func NewMetaData(id, root string, info ToolInfo) *MetaData {`
	//       ^^^^^^^^^^^

	withTestDatabase(t, func(db *Database) {
		if actual, err := db.MonikersByPosition("protocol/protocol.go", 92, 10); err != nil {
			t.Errorf("unexpected error %s", err)
		} else {
			expected := [][]MonikerData{
				{
					{
						Kind:                 "export",
						Scheme:               "gomod",
						Identifier:           "github.com/sourcegraph/lsif-go/protocol:NewMetaData",
						PackageInformationID: ID("213"),
					},
				},
			}

			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("unexpected moniker result. want=%v have=%v", expected, actual)
			}
		}
	})
}

func TestDatabaseMonikerResults(t *testing.T) {
	edgeLocations := []InternalLocation{
		{
			Path:  "protocol/protocol.go",
			Range: newRange(600, 1, 600, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(644, 1, 644, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(507, 1, 507, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(553, 1, 553, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(462, 1, 462, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(484, 1, 484, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(410, 5, 410, 9),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(622, 1, 622, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(440, 1, 440, 5),
		},
		{
			Path:  "protocol/protocol.go",
			Range: newRange(530, 1, 530, 5),
		},
	}

	markdownLocations := []InternalLocation{
		{
			Path:  "internal/index/helper.go",
			Range: newRange(78, 6, 78, 16),
		},
	}

	testCases := []struct {
		tableName          string
		scheme             string
		identifier         string
		skip               int
		take               int
		expectedLocations  []InternalLocation
		expectedTotalCount int
	}{
		{"definitions", "gomod", "github.com/sourcegraph/lsif-go/protocol:Edge", 0, 100, edgeLocations, 10},
		{"definitions", "gomod", "github.com/sourcegraph/lsif-go/protocol:Edge", 3, 4, edgeLocations[3:7], 10},
		{"references", "gomod", "github.com/slimsag/godocmd:ToMarkdown", 0, 100, markdownLocations, 1},
	}

	withTestDatabase(t, func(db *Database) {
		for i, testCase := range testCases {
			if actual, totalCount, err := db.MonikerResults(testCase.tableName, testCase.scheme, testCase.identifier, testCase.skip, testCase.take); err != nil {
				t.Errorf("unexpected error for test case #%d: %s", i, err)
			} else {
				if totalCount != testCase.expectedTotalCount {
					t.Errorf("unexpected moniker result total count for test case #%d. want=%v have=%v", i, testCase.expectedTotalCount, totalCount)
				}

				if !reflect.DeepEqual(actual, testCase.expectedLocations) {
					t.Errorf("unexpected moniker result locations for test case #%d. want=%v have=%v", i, testCase.expectedLocations, actual)
				}
			}
		}
	})
}

func TestDatabasePackageInformation(t *testing.T) {
	withTestDatabase(t, func(db *Database) {
		if actual, exists, err := db.PackageInformation("protocol/protocol.go", ID("213")); err != nil {
			t.Errorf("unexpected error %s", err)
		} else if !exists {
			t.Errorf("no package information")
		} else {
			expected := PackageInformationData{
				Name:    "github.com/sourcegraph/lsif-go",
				Version: "v0.0.0-ad3507cbeb18",
			}

			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("unexpected package information. want=%v have=%v", expected, actual)
			}
		}
	})
}

func withTestDatabase(t *testing.T, testFunc func(db *Database)) {
	documentDataCache, err := NewDocumentDataCache(5)
	if err != nil {
		t.Fatalf("failed to create document data cache: %s", err)
	}

	resultChunkDataCache, err := NewResultChunkDataCache(5)
	if err != nil {
		t.Fatalf("failed to create result chunk data cache: %s", err)
	}

	// TODO - how to find this in general?
	db, err := OpenDatabase("lsif-go@ad3507cb.lsif.db", documentDataCache, resultChunkDataCache)
	if err != nil {
		t.Fatalf("failed to open database: %s", err)
	}
	defer db.Close()

	testFunc(db)
}