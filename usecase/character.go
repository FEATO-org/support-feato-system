package usecase

import "github.com/FEATO-org/support-feato-system/domain/model"

type CharacterGenerator interface {
	Generate(*model.Character) (*model.Character, error)
}
type Character struct{ generator CharacterGenerator }

func NewCharacter(g CharacterGenerator) *Character { return &Character{generator: g} }

func (du *Character) CharacterGenerate(gender string) (*model.Character, error) {
	character, err := model.NewCharacter(gender, "", "", "", "")
	if err != nil {
		return nil, err
	}
	return du.generator.Generate(character)
}
