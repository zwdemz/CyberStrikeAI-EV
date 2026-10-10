package skillcatalog_test

import (
	"context"
	"cyberstrike-ai/internal/skillcatalog"
	"errors"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"testing"
)

type backend struct{ err error }

func (b *backend) List(context.Context) ([]skill.FrontMatter, error) {
	return []skill.FrontMatter{{Name: "ctf-web"}, {Name: "api-sec"}, {Name: "pentest-agent-os"}}, b.err
}
func (b *backend) Get(_ context.Context, name string) (skill.Skill, error) {
	return skill.Skill{FrontMatter: skill.FrontMatter{Name: name}}, b.err
}
func TestDiscoveryAndExplicitSpecialist(t *testing.T) {
	b, err := skillcatalog.New(&backend{}, "src")
	if err != nil {
		t.Fatal(err)
	}
	items, err := b.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "pentest-agent-os" || items[1].Name != "api-sec" {
		t.Fatalf("unexpected catalog: %v", items)
	}
	specialist, err := b.Get(context.Background(), "ctf-web")
	if err != nil || specialist.Name != "ctf-web" {
		t.Fatal("explicit specialist unavailable")
	}
}
func TestCompatibilityAndErrors(t *testing.T) {
	original := &backend{}
	for _, profile := range []string{"", "all", " ALL "} {
		b, err := skillcatalog.New(original, profile)
		if err != nil || b != original {
			t.Fatal("compatibility failed")
		}
	}
	if _, err := skillcatalog.New(original, "typo"); err == nil {
		t.Fatal("invalid profile accepted")
	}
	if _, err := skillcatalog.New(nil, "src"); err == nil {
		t.Fatal("nil backend accepted")
	}
	failure := errors.New("unavailable")
	b, _ := skillcatalog.New(&backend{err: failure}, "src")
	if _, err := b.List(context.Background()); !errors.Is(err, failure) {
		t.Fatal("list error lost")
	}
	if _, err := b.Get(context.Background(), "ctf-web"); !errors.Is(err, failure) {
		t.Fatal("get error lost")
	}
}
