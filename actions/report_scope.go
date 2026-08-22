package actions

import (
	"fmt"
	"net/http"

	"creaves-console/models"
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/pop/v6"
)

type ReportScope struct{ InstanceID string }

func ResolveReportScope(instanceID string) ReportScope { return ReportScope{InstanceID: instanceID} }
func (s ReportScope) IsGlobal() bool                   { return s.InstanceID == "" }
func (s ReportScope) SQL() (string, []interface{}) {
	if s.IsGlobal() {
		return "", nil
	}
	return " instance_id = ? ", []interface{}{s.InstanceID}
}
func (s ReportScope) String() string {
	if s.IsGlobal() {
		return "global"
	}
	return fmt.Sprintf("instance:%s", s.InstanceID)
}

// ScopedWhere appends instance predicate to an existing WHERE fragment.
func ScopedWhere(scope ReportScope, base string) (string, []interface{}) {
	if scope.IsGlobal() {
		return base, nil
	}
	if base == "" {
		return "WHERE instance_id = ?", []interface{}{scope.InstanceID}
	}
	return base + " AND instance_id = ?", []interface{}{scope.InstanceID}
}

func scopedWhere(scope ReportScope, base string) (string, []interface{}) {
	if scope.IsGlobal() {
		return base, nil
	}
	if base == "" {
		return "WHERE instance_id = ?", []interface{}{scope.InstanceID}
	}
	if len(base) >= 5 && base[:5] == "WHERE" {
		return base + " AND instance_id = ?", []interface{}{scope.InstanceID}
	}
	return "WHERE " + base + " AND instance_id = ?", []interface{}{scope.InstanceID}
}

func reportScope(c buffalo.Context, tx *pop.Connection) (ReportScope, error) {
	scope := ResolveReportScope(c.Param("instance_id"))
	if scope.IsGlobal() {
		return scope, nil
	}
	if exists, err := tx.Where("instance_id = ?", scope.InstanceID).Exists(&models.CreavesInstance{}); err != nil {
		return scope, err
	} else if !exists {
		return scope, c.Error(http.StatusNotFound, fmt.Errorf("unknown instance: %s", scope.InstanceID))
	}
	return scope, nil
}
