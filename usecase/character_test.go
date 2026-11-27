package usecase

import (
	"errors"
	"github.com/FEATO-org/support-feato-system/domain/model"
	"testing"
)

type fakeCharacterGenerator struct{ err error }

func (f fakeCharacterGenerator) Generate(c *model.Character) (*model.Character, error) {
	return c, f.err
}
func TestCharacterGenerate(t *testing.T) {
	c, err := NewCharacter(fakeCharacterGenerator{}).CharacterGenerate("female")
	if err != nil || c.GetGender() != "female" {
		t.Fatalf("got %v, %v", c, err)
	}
	expected := errors.New("generator failed")
	_, err = NewCharacter(fakeCharacterGenerator{err: expected}).CharacterGenerate("")
	if !errors.Is(err, expected) {
		t.Fatalf("got %v", err)
	}
}
