package roles

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrDuplicateRoleName    = errors.New("roles: duplicate role name")
	ErrRoleNotFound         = errors.New("roles: role not found")
	ErrBuiltInRoleImmutable = errors.New("roles: built-in role templates are immutable")
)

type RoleStore struct {
	mu      sync.RWMutex
	builtIn map[string]RoleTemplate
	custom  map[string]RoleTemplate
}

func NewRoleStore(customTemplates []RoleTemplate) (*RoleStore, error) {
	store := &RoleStore{
		builtIn: make(map[string]RoleTemplate),
		custom:  make(map[string]RoleTemplate),
	}

	for _, template := range BuiltInTemplates() {
		store.builtIn[template.Name] = cloneTemplate(template)
	}

	for _, template := range customTemplates {
		normalized, err := normalizeTemplate(template, false)
		if err != nil {
			return nil, fmt.Errorf("roles: invalid custom template %q: %w", template.Name, err)
		}
		if _, exists := store.builtIn[normalized.Name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateRoleName, normalized.Name)
		}
		if _, exists := store.custom[normalized.Name]; exists {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateRoleName, normalized.Name)
		}
		store.custom[normalized.Name] = cloneTemplate(normalized)
	}

	return store, nil
}

func (s *RoleStore) List() []RoleTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]RoleTemplate, 0, len(s.builtIn)+len(s.custom))
	for _, template := range s.builtIn {
		all = append(all, cloneTemplate(template))
	}
	for _, template := range s.custom {
		all = append(all, cloneTemplate(template))
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Name < all[j].Name
	})

	return all
}

func (s *RoleStore) Get(name string) (RoleTemplate, bool) {
	normalizedName := normalizeRoleName(name)
	if normalizedName == "" {
		return RoleTemplate{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if template, ok := s.builtIn[normalizedName]; ok {
		return cloneTemplate(template), true
	}
	if template, ok := s.custom[normalizedName]; ok {
		return cloneTemplate(template), true
	}

	return RoleTemplate{}, false
}

func (s *RoleStore) CreateCustom(template RoleTemplate) error {
	normalized, err := normalizeTemplate(template, false)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.builtIn[normalized.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateRoleName, normalized.Name)
	}
	if _, exists := s.custom[normalized.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateRoleName, normalized.Name)
	}

	s.custom[normalized.Name] = cloneTemplate(normalized)
	return nil
}

func (s *RoleStore) UpdateCustom(template RoleTemplate) error {
	normalized, err := normalizeTemplate(template, false)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.builtIn[normalized.Name]; exists {
		return fmt.Errorf("%w: %s", ErrBuiltInRoleImmutable, normalized.Name)
	}
	if _, exists := s.custom[normalized.Name]; !exists {
		return fmt.Errorf("%w: %s", ErrRoleNotFound, normalized.Name)
	}

	s.custom[normalized.Name] = cloneTemplate(normalized)
	return nil
}

func (s *RoleStore) Delete(name string) error {
	normalizedName := normalizeRoleName(name)
	if normalizedName == "" {
		return ErrRoleNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.builtIn[normalizedName]; exists {
		return fmt.Errorf("%w: %s", ErrBuiltInRoleImmutable, normalizedName)
	}
	if _, exists := s.custom[normalizedName]; !exists {
		return fmt.Errorf("%w: %s", ErrRoleNotFound, normalizedName)
	}

	delete(s.custom, normalizedName)
	return nil
}
