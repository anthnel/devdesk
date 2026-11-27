// Command engine-survey prints the Summary of the survey file the processes of
// a test suite appended to (engineguard.SurveyEnv). The mise task engine-survey
// runs it after `go test ./...`.
package main

import (
	"fmt"
	"os"

	"github.com/anthnel/devdesk/internal/engineguard"
)

func main() {
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: engine-survey <survey file>")
		os.Exit(2)
	}
	hits, err := engineguard.ReadSurvey(os.Args[1])
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "engine-survey: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(engineguard.Summary(hits))
}
