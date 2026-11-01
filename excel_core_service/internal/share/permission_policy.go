package share

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type permissionConfig struct {
	Roles map[string]struct {
		Name    string   `json:"name"`
		Actions []string `json:"actions"`
	} `json:"roles"`
}

type permissionPolicy struct {
	mu      sync.Mutex
	path    string
	modTime time.Time
	size    int64
	roles   map[string]map[string]struct{}
}

func newPermissionPolicy(path string) (*permissionPolicy, error) {
	resolved, err := permissionConfigFile(path)
	if err != nil {
		return nil, err
	}
	policy := &permissionPolicy{path: resolved}
	if err := policy.reload(); err != nil {
		return nil, err
	}
	return policy, nil
}

func permissionConfigFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("permission config path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat permission config path %q: %w", path, err)
	}
	if info.IsDir() {
		path = filepath.Join(path, "permissions.json")
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve permission config path: %w", err)
	}
	return resolved, nil
}

func (policy *permissionPolicy) current() (map[string]map[string]struct{}, error) {
	policy.mu.Lock()
	defer policy.mu.Unlock()

	info, err := os.Stat(policy.path)
	if err != nil {
		return nil, fmt.Errorf("stat permission config: %w", err)
	}
	if !info.ModTime().Equal(policy.modTime) || info.Size() != policy.size {
		if err := policy.reloadLocked(info); err != nil {
			return nil, err
		}
	}
	return policy.roles, nil
}

func (policy *permissionPolicy) reload() error {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	info, err := os.Stat(policy.path)
	if err != nil {
		return fmt.Errorf("stat permission config: %w", err)
	}
	return policy.reloadLocked(info)
}

func (policy *permissionPolicy) reloadLocked(info os.FileInfo) error {
	payload, err := os.ReadFile(policy.path)
	if err != nil {
		return fmt.Errorf("read permission config: %w", err)
	}
	var config permissionConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return fmt.Errorf("decode permission config: %w", err)
	}
	roles, err := validatePermissionRoles(config.Roles)
	if err != nil {
		return err
	}
	policy.roles = roles
	policy.modTime = info.ModTime()
	policy.size = info.Size()
	return nil
}

func validatePermissionRoles(configRoles map[string]struct {
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}) (map[string]map[string]struct{}, error) {
	requiredRoles := []string{"manager", "editor", "filler", "viewer"}
	roles := make(map[string]map[string]struct{}, len(requiredRoles))
	for _, role := range requiredRoles {
		definition, ok := configRoles[role]
		if !ok {
			return nil, fmt.Errorf("permission config missing role %q", role)
		}
		actions := make(map[string]struct{}, len(definition.Actions))
		for _, action := range definition.Actions {
			action = strings.ToLower(strings.TrimSpace(action))
			if action == "" {
				return nil, fmt.Errorf("permission config role %q has empty action", role)
			}
			actions[action] = struct{}{}
		}
		roles[role] = actions
	}
	return roles, nil
}
