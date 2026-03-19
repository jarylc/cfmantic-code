package filesync

import (
	"fmt"
	"strings"
)

func ExactIDListFilter(ids ...string) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}

	return fmt.Sprintf("id in [%s]", strings.Join(quoted, ","))
}
