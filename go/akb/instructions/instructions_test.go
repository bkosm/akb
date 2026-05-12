package instructions

import (
	"strings"
	"testing"
)

func TestBaseReturnsEmbeddedMarkdown(t *testing.T) {
	t.Parallel()

	text := Base()
	if text == "" {
		t.Fatal("Base should not be empty")
	}
	for _, section := range []string{
		"Workflow:",
		"Backup workflow:",
		"Remote KB durability contract:",
		"Mount path convention:",
	} {
		if !strings.Contains(text, section) {
			t.Fatalf("Base missing section %q", section)
		}
	}
}

func TestBuildAppendsBackendInfo(t *testing.T) {
	t.Parallel()

	base := Base()
	if got := Build(""); got != base {
		t.Fatal("Build without backend info should return Base")
	}

	got := Build("arn:aws:s3:eu-west-1:123:akb/config.json")
	if !strings.HasPrefix(got, base+"\n\n") {
		t.Fatal("Build should append backend info after base instructions")
	}
	if !strings.HasSuffix(got, "Config backend: arn:aws:s3:eu-west-1:123:akb/config.json") {
		t.Fatalf("Build output missing backend info: %q", got)
	}
}
