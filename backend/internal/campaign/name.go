package campaign

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/emiralpayar/campaign-composer/internal/verr"
	"github.com/google/uuid"
)

const (
	// defaultNameRunes is the length of a name derived from the brief.
	defaultNameRunes = 60
	// MaxNameRunes bounds a name set by the marketer.
	MaxNameRunes = 120
)

// DefaultName derives a campaign name from its brief: whitespace collapsed,
// at most 60 characters cut at a word boundary, "…" appended when cut. The
// backfill in migrations/00002_campaign_name.sql implements the same rule.
func DefaultName(brief string) string {
	b := strings.Join(strings.Fields(brief), " ")
	r := []rune(b)
	if len(r) <= defaultNameRunes {
		return b
	}
	// Up to 61 characters: a space right after the 60th keeps the last word.
	cut := string(r[:defaultNameRunes+1])
	if i := strings.LastIndexByte(cut, ' '); i >= 0 {
		cut = cut[:i]
	}
	if utf8.RuneCountInString(cut) > defaultNameRunes { // one long word
		cut = string(r[:defaultNameRunes])
	}
	return strings.TrimRight(cut, " ,.;:-–") + "…"
}

// ValidateName trims a marketer-entered name and checks it is non-empty and
// at most MaxNameRunes characters.
func ValidateName(name string) (string, verr.List) {
	name = strings.Join(strings.Fields(name), " ")
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return "", verr.List{{Path: "name", Code: "empty_field", Message: "Kampanya adı boş olamaz"}}
	case n > MaxNameRunes:
		return "", verr.List{{Path: "name", Code: "too_long", Message: "Kampanya adı en fazla 120 karakter olabilir"}}
	}
	return name, nil
}

// Rename sets the campaign's name. It is cosmetic, so it is allowed in every
// status (also after approval).
func (s *Store) Rename(ctx context.Context, id uuid.UUID, name string) (Campaign, error) {
	norm, errs := ValidateName(name)
	if errs != nil {
		return Campaign{}, errs
	}
	return scan(s.pool.QueryRow(ctx,
		`update campaigns set name=$2, updated_at=now() where id=$1 returning `+selectCols, id, norm))
}
