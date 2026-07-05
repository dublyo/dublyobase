package apis

import (
	"net/http"
	"time"

	"github.com/dublyo/dublyobase/core"
)

func (s *server) appListOrganizations(w http.ResponseWriter, r *http.Request) {
	project, user, ok := s.resolveAppUser(w, r)
	if !ok {
		return
	}
	orgs, err := core.ListOrganizationsForUser(r.Context(), s.app.Pool, project, user)
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": orgs})
}

func (s *server) appCreateOrganization(w http.ResponseWriter, r *http.Request) {
	project, user, ok := s.resolveAppUser(w, r)
	if !ok {
		return
	}
	var req core.CreateOrganizationInput
	if !decodeJSON(w, r, &req) {
		return
	}
	org, err := core.CreateOrganization(r.Context(), s.app.Pool, project, user, req, s.clientIP(r), r.UserAgent(), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (s *server) appListOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	project, user, ok := s.resolveAppUser(w, r)
	if !ok {
		return
	}
	members, err := core.ListOrganizationMembers(r.Context(), s.app.Pool, project, user, r.PathValue("orgId"))
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": members})
}

func (s *server) appCreateOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	project, user, ok := s.resolveAppUser(w, r)
	if !ok {
		return
	}
	var req core.CreateOrganizationInvitationInput
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := core.CreateOrganizationInvitation(r.Context(), s.app.Pool, s.app.Config, project, user, r.PathValue("orgId"), req, s.clientIP(r), r.UserAgent(), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	s.deliverAuthTokenEmail(r.Context(), project.Slug, &core.AuthTokenRequestResult{
		Accepted:    true,
		Email:       result.Email,
		Token:       result.Token,
		Type:        result.Type,
		ProjectName: result.ProjectName,
	})
	writeJSON(w, http.StatusCreated, result)
}

func (s *server) appAcceptOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	project, user, ok := s.resolveAppUser(w, r)
	if !ok {
		return
	}
	var req core.AcceptOrganizationInvitationInput
	if !decodeJSON(w, r, &req) {
		return
	}
	member, err := core.AcceptOrganizationInvitation(r.Context(), s.app.Pool, project, user, req, s.clientIP(r), r.UserAgent(), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, member)
}

func (s *server) resolveAppUser(w http.ResponseWriter, r *http.Request) (*core.Project, *core.AppUser, bool) {
	if !s.checkProjectQuota(w, r, r.PathValue("slug"), true) {
		return nil, nil, false
	}
	project, user, err := core.ResolveAppAccessToken(r.Context(), s.app.Pool, s.app.Config, r.PathValue("slug"), bearerToken(r), time.Now())
	if err != nil {
		writeCoreError(w, err)
		return nil, nil, false
	}
	return project, user, true
}
