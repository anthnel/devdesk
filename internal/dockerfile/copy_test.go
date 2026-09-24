package dockerfile

import "testing"

func TestCopiesAreReadPerStage(t *testing.T) {
	src := "FROM golang:1.23 AS build\n" +
		"COPY . /src\n" +
		"FROM debian:12\n" +
		"COPY --from=build /src/app /app\n" +
		"ADD --chown=1000 \\\n    ./ \\\n    /data/\n"
	f := Parse([]byte(src))
	if len(f.Stages) != 2 {
		t.Fatalf("got %d stages", len(f.Stages))
	}
	build := f.Stages[0].Copies
	if len(build) != 1 || build[0].Line != 2 || build[0].EndLine != 2 || !build[0].TakesWholeContext() {
		t.Errorf("build stage copies = %+v", build)
	}
	final := f.Stages[1].Copies
	if len(final) != 2 {
		t.Fatalf("final stage copies = %+v", final)
	}
	if final[0].From != "build" || final[0].TakesWholeContext() {
		t.Errorf("a COPY --from reads another stage, not the context: %+v", final[0])
	}
	add := final[1]
	if add.Keyword != "ADD" || add.Line != 5 || add.EndLine != 7 || !add.TakesWholeContext() {
		t.Errorf("continued ADD = %+v, want lines 5-7 taking the whole context", add)
	}
}

func TestWhatTakesTheWholeContext(t *testing.T) {
	for _, tt := range []struct {
		line string
		want bool
	}{
		{"COPY . .", true},
		{"COPY ./ /app/", true},
		{"COPY package.json . /app/", true},
		{`COPY [".", "/app"]`, true},
		{"COPY src /app", false},
		{"COPY * /app/", false}, // whether * takes dot files depends on the builder
		{"COPY --exclude=.git . /app", false},
		{"COPY --from=builder . /app", false},
		{"COPY <<EOF /etc/conf\nx\nEOF", false},
		{"COPY .", false},
		{`COPY [".", `, false},
	} {
		f := Parse([]byte("FROM alpine\n" + tt.line + "\n"))
		copies := f.Stages[0].Copies
		if len(copies) == 0 {
			t.Errorf("%q: not read as a copy", tt.line)
			continue
		}
		if got := copies[0].TakesWholeContext(); got != tt.want {
			t.Errorf("%q: TakesWholeContext = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestACopyBeforeAnyFromIsInNoStage(t *testing.T) {
	f := Parse([]byte("COPY . .\nFROM alpine\n"))
	if len(f.Stages) != 1 || len(f.Stages[0].Copies) != 0 {
		t.Errorf("stages = %+v", f.Stages)
	}
}

// BuildKit's per-Dockerfile ignore file starts like a Dockerfile name and is
// not one.
func TestADockerfileIgnoreFileIsNotADockerfile(t *testing.T) {
	for name, want := range map[string]bool{
		"Dockerfile":                  true,
		"Dockerfile.dev":              true,
		"api.Dockerfile":              true,
		"Dockerfile.dockerignore":     false,
		"Dockerfile.dev.dockerignore": false,
	} {
		if got := IsDockerfileName(name); got != want {
			t.Errorf("IsDockerfileName(%q) = %v, want %v", name, got, want)
		}
	}
}
