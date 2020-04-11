package query

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func prettyPrint(nodes []Node) string {
	var resultStr []string
	for _, node := range nodes {
		resultStr = append(resultStr, node.String())
	}
	return strings.Join(resultStr, " ")
}

func Test_LowercaseFieldNames(t *testing.T) {
	input := "rEpO:foo PATTERN"
	want := `(and "repo:foo" "PATTERN")`
	query, _ := parseAndOr(input)
	got := prettyPrint(LowercaseFieldNames(query))
	if diff := cmp.Diff(got, want); diff != "" {
		t.Fatal(diff)

	}
}

func Test_RightAssociatePatterns(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{
			input: "repo:foo a or b",
			want:  `(or (and "repo:foo" "a") "b")`,
		},
	}
	for _, tt := range cases {
		t.Run("right assiocate patterns", func(t *testing.T) {
			query, _ := parseAndOr(tt.input)
			got := prettyPrint(LowercaseFieldNames(query))
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Fatal(diff)

			}
		})
	}
}
