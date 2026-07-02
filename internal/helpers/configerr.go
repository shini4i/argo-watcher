package helpers

import (
	"errors"
	"sort"
	"strings"

	envConfig "github.com/caarlos0/env/v11"
)

// PrettifyEnvError reformats github.com/caarlos0/env's semicolon-joined
// AggregateError into a multi-line message that groups missing required
// vars and invalid values under separate headers, so an operator can fix
// every reported problem in one pass instead of once per restart. Errors
// that are not AggregateError pass through unchanged. The leadIn becomes
// the first line of the formatted output.
func PrettifyEnvError(err error, leadIn string) error {
	var agg envConfig.AggregateError
	if !errors.As(err, &agg) {
		return err
	}

	var missing, invalid []string
	for _, e := range agg.Errors {
		var notSet envConfig.VarIsNotSetError
		if errors.As(e, &notSet) {
			missing = append(missing, "  - "+notSet.Key)
			continue
		}
		invalid = append(invalid, "  - "+e.Error())
	}
	sort.Strings(missing)
	sort.Strings(invalid)

	var sb strings.Builder
	sb.WriteString(leadIn)
	if len(missing) > 0 {
		sb.WriteString("\nmissing required environment variables:\n")
		sb.WriteString(strings.Join(missing, "\n"))
	}
	if len(invalid) > 0 {
		sb.WriteString("\ninvalid values:\n")
		sb.WriteString(strings.Join(invalid, "\n"))
	}
	return errors.New(sb.String())
}
