package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ctx() context.Context { return context.Background() }

func makeUser(email string) *User {
	return &User{Email: email, DisplayName: "Test", Active: true}
}

func makeCompany(slug string, ownerID uuid.UUID) *Company {
	return &Company{Name: slug, Slug: slug, OwnerID: ownerID}
}

func makeProject(companyID uuid.UUID, slug string) *Project {
	return &Project{CompanyID: companyID, Name: slug, Slug: slug}
}

func makeWorkflow(projectID uuid.UUID, slug string) *WorkflowRecord {
	return &WorkflowRecord{
		ProjectID:  projectID,
		Name:       slug,
		Slug:       slug,
		ConfigYAML: "version: 1",
		Status:     WorkflowStatusDraft,
		CreatedBy:  uuid.New(),
		UpdatedBy:  uuid.New(),
	}
}

// ===========================================================================
// UserStore Tests
// ===========================================================================

func TestMockUserStore_Create(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("alice@test.com")
	if err := s.Create(ctx(), u); err != nil {
		t.Fatal(err)
	}
	if u.ID == uuid.Nil {
		t.Fatal("expected ID to be assigned")
	}
	if u.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestMockUserStore_Create_DuplicateEmail(t *testing.T) {
	s := NewMockUserStore()
	_ = s.Create(ctx(), makeUser("dup@test.com"))
	err := s.Create(ctx(), makeUser("dup@test.com"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockUserStore_Create_WithExistingID(t *testing.T) {
	s := NewMockUserStore()
	id := uuid.New()
	u := makeUser("preset@test.com")
	u.ID = id
	if err := s.Create(ctx(), u); err != nil {
		t.Fatal(err)
	}
	if u.ID != id {
		t.Fatalf("expected ID %v, got %v", id, u.ID)
	}
}

func TestMockUserStore_Get(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("get@test.com")
	_ = s.Create(ctx(), u)
	got, err := s.Get(ctx(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "get@test.com" {
		t.Fatalf("expected email get@test.com, got %s", got.Email)
	}
}

func TestMockUserStore_Get_NotFound(t *testing.T) {
	s := NewMockUserStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockUserStore_Get_ReturnsCopy(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("copy@test.com")
	_ = s.Create(ctx(), u)
	got1, _ := s.Get(ctx(), u.ID)
	got1.Email = "modified@test.com"
	got2, _ := s.Get(ctx(), u.ID)
	if got2.Email != "copy@test.com" {
		t.Fatal("Get should return a copy, mutations should not affect stored data")
	}
}

func TestMockUserStore_GetByEmail(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("byemail@test.com")
	_ = s.Create(ctx(), u)
	got, err := s.GetByEmail(ctx(), "byemail@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatalf("expected ID %v, got %v", u.ID, got.ID)
	}
}

func TestMockUserStore_GetByEmail_NotFound(t *testing.T) {
	s := NewMockUserStore()
	_, err := s.GetByEmail(ctx(), "nope@test.com")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockUserStore_GetByOAuth(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("oauth@test.com")
	u.OAuthProvider = OAuthProviderGitHub
	u.OAuthID = "gh-123"
	_ = s.Create(ctx(), u)
	got, err := s.GetByOAuth(ctx(), OAuthProviderGitHub, "gh-123")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatalf("expected ID %v", u.ID)
	}
}

func TestMockUserStore_GetByOAuth_NotFound(t *testing.T) {
	s := NewMockUserStore()
	_, err := s.GetByOAuth(ctx(), OAuthProviderGitHub, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockUserStore_Update(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("update@test.com")
	_ = s.Create(ctx(), u)
	u.DisplayName = "Updated"
	if err := s.Update(ctx(), u); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx(), u.ID)
	if got.DisplayName != "Updated" {
		t.Fatalf("expected Updated, got %s", got.DisplayName)
	}
}

func TestMockUserStore_Update_NotFound(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("nope@test.com")
	u.ID = uuid.New()
	err := s.Update(ctx(), u)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockUserStore_Update_DuplicateEmail(t *testing.T) {
	s := NewMockUserStore()
	u1 := makeUser("first@test.com")
	u2 := makeUser("second@test.com")
	_ = s.Create(ctx(), u1)
	_ = s.Create(ctx(), u2)
	u2.Email = "first@test.com"
	err := s.Update(ctx(), u2)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockUserStore_Delete(t *testing.T) {
	s := NewMockUserStore()
	u := makeUser("del@test.com")
	_ = s.Create(ctx(), u)
	if err := s.Delete(ctx(), u.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), u.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found after delete")
	}
}

func TestMockUserStore_Delete_NotFound(t *testing.T) {
	s := NewMockUserStore()
	err := s.Delete(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockUserStore_List(t *testing.T) {
	s := NewMockUserStore()
	_ = s.Create(ctx(), makeUser("a@test.com"))
	_ = s.Create(ctx(), makeUser("b@test.com"))
	_ = s.Create(ctx(), makeUser("c@test.com"))
	list, err := s.List(ctx(), UserFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
}

func TestMockUserStore_List_FilterByEmail(t *testing.T) {
	s := NewMockUserStore()
	_ = s.Create(ctx(), makeUser("target@test.com"))
	_ = s.Create(ctx(), makeUser("other@test.com"))
	list, _ := s.List(ctx(), UserFilter{Email: "target@test.com"})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Email != "target@test.com" {
		t.Fatalf("expected target@test.com, got %s", list[0].Email)
	}
}

func TestMockUserStore_List_FilterByActive(t *testing.T) {
	s := NewMockUserStore()
	u1 := makeUser("active@test.com")
	u1.Active = true
	u2 := makeUser("inactive@test.com")
	u2.Active = false
	_ = s.Create(ctx(), u1)
	_ = s.Create(ctx(), u2)
	list, _ := s.List(ctx(), UserFilter{Active: new(true)})
	if len(list) != 1 {
		t.Fatalf("expected 1 active, got %d", len(list))
	}
}

func TestMockUserStore_List_Pagination(t *testing.T) {
	s := NewMockUserStore()
	for i := range 10 {
		_ = s.Create(ctx(), makeUser("pag"+string(rune('a'+i))+"@test.com"))
	}
	list, _ := s.List(ctx(), UserFilter{Pagination: Pagination{Offset: 2, Limit: 3}})
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
}

func TestMockUserStore_List_PaginationOffsetBeyondEnd(t *testing.T) {
	s := NewMockUserStore()
	_ = s.Create(ctx(), makeUser("only@test.com"))
	list, _ := s.List(ctx(), UserFilter{Pagination: Pagination{Offset: 100, Limit: 10}})
	if len(list) != 0 {
		t.Fatalf("expected 0, got %d", len(list))
	}
}

func TestMockUserStore_List_PaginationZeroLimit(t *testing.T) {
	s := NewMockUserStore()
	_ = s.Create(ctx(), makeUser("one@test.com"))
	_ = s.Create(ctx(), makeUser("two@test.com"))
	list, _ := s.List(ctx(), UserFilter{Pagination: Pagination{Offset: 0, Limit: 0}})
	if len(list) != 2 {
		t.Fatalf("expected 2 (no limit), got %d", len(list))
	}
}

// ===========================================================================
// CompanyStore Tests
// ===========================================================================

func TestMockCompanyStore_Create(t *testing.T) {
	s := NewMockCompanyStore()
	c := makeCompany("acme", uuid.New())
	if err := s.Create(ctx(), c); err != nil {
		t.Fatal(err)
	}
	if c.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockCompanyStore_Create_DuplicateSlug(t *testing.T) {
	s := NewMockCompanyStore()
	_ = s.Create(ctx(), makeCompany("dup", uuid.New()))
	err := s.Create(ctx(), makeCompany("dup", uuid.New()))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockCompanyStore_Get(t *testing.T) {
	s := NewMockCompanyStore()
	c := makeCompany("getco", uuid.New())
	_ = s.Create(ctx(), c)
	got, err := s.Get(ctx(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "getco" {
		t.Fatal("slug mismatch")
	}
}

func TestMockCompanyStore_Get_NotFound(t *testing.T) {
	s := NewMockCompanyStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockCompanyStore_GetBySlug(t *testing.T) {
	s := NewMockCompanyStore()
	c := makeCompany("slugco", uuid.New())
	_ = s.Create(ctx(), c)
	got, err := s.GetBySlug(ctx(), "slugco")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID {
		t.Fatal("ID mismatch")
	}
}

func TestMockCompanyStore_GetBySlug_NotFound(t *testing.T) {
	s := NewMockCompanyStore()
	_, err := s.GetBySlug(ctx(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockCompanyStore_Update(t *testing.T) {
	s := NewMockCompanyStore()
	c := makeCompany("upco", uuid.New())
	_ = s.Create(ctx(), c)
	c.Name = "Updated Company"
	if err := s.Update(ctx(), c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx(), c.ID)
	if got.Name != "Updated Company" {
		t.Fatal("name not updated")
	}
}

func TestMockCompanyStore_Update_NotFound(t *testing.T) {
	s := NewMockCompanyStore()
	c := makeCompany("ghost", uuid.New())
	c.ID = uuid.New()
	err := s.Update(ctx(), c)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockCompanyStore_Update_DuplicateSlug(t *testing.T) {
	s := NewMockCompanyStore()
	c1 := makeCompany("slug1", uuid.New())
	c2 := makeCompany("slug2", uuid.New())
	_ = s.Create(ctx(), c1)
	_ = s.Create(ctx(), c2)
	c2.Slug = "slug1"
	err := s.Update(ctx(), c2)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockCompanyStore_Delete(t *testing.T) {
	s := NewMockCompanyStore()
	c := makeCompany("delco", uuid.New())
	_ = s.Create(ctx(), c)
	if err := s.Delete(ctx(), c.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), c.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found after delete")
	}
}

func TestMockCompanyStore_Delete_NotFound(t *testing.T) {
	s := NewMockCompanyStore()
	err := s.Delete(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockCompanyStore_List(t *testing.T) {
	s := NewMockCompanyStore()
	_ = s.Create(ctx(), makeCompany("a-co", uuid.New()))
	_ = s.Create(ctx(), makeCompany("b-co", uuid.New()))
	list, _ := s.List(ctx(), CompanyFilter{})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockCompanyStore_List_FilterByOwnerID(t *testing.T) {
	s := NewMockCompanyStore()
	ownerID := uuid.New()
	_ = s.Create(ctx(), makeCompany("mine", ownerID))
	_ = s.Create(ctx(), makeCompany("theirs", uuid.New()))
	list, _ := s.List(ctx(), CompanyFilter{OwnerID: &ownerID})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockCompanyStore_ListForUser_ByOwnership(t *testing.T) {
	s := NewMockCompanyStore()
	ownerID := uuid.New()
	_ = s.Create(ctx(), makeCompany("owned", ownerID))
	_ = s.Create(ctx(), makeCompany("other", uuid.New()))
	list, _ := s.ListForUser(ctx(), ownerID)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockCompanyStore_ListForUser_ByMembership(t *testing.T) {
	cs := NewMockCompanyStore()
	ms := NewMockMembershipStore()
	cs.SetMembershipStore(ms)
	userID := uuid.New()
	c := makeCompany("member-co", uuid.New())
	_ = cs.Create(ctx(), c)
	_ = ms.Create(ctx(), &Membership{UserID: userID, CompanyID: c.ID, Role: RoleViewer})
	list, _ := cs.ListForUser(ctx(), userID)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

// ===========================================================================
// ProjectStore Tests
// ===========================================================================

func TestMockProjectStore_Create(t *testing.T) {
	s := NewMockProjectStore()
	p := makeProject(uuid.New(), "proj1")
	if err := s.Create(ctx(), p); err != nil {
		t.Fatal(err)
	}
	if p.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockProjectStore_Create_DuplicateSlugSameCompany(t *testing.T) {
	s := NewMockProjectStore()
	companyID := uuid.New()
	_ = s.Create(ctx(), makeProject(companyID, "dup"))
	err := s.Create(ctx(), makeProject(companyID, "dup"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockProjectStore_Create_SameSlugDifferentCompany(t *testing.T) {
	s := NewMockProjectStore()
	_ = s.Create(ctx(), makeProject(uuid.New(), "same"))
	err := s.Create(ctx(), makeProject(uuid.New(), "same"))
	if err != nil {
		t.Fatalf("same slug in different company should be allowed, got %v", err)
	}
}

func TestMockProjectStore_Get(t *testing.T) {
	s := NewMockProjectStore()
	p := makeProject(uuid.New(), "get-proj")
	_ = s.Create(ctx(), p)
	got, err := s.Get(ctx(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "get-proj" {
		t.Fatal("slug mismatch")
	}
}

func TestMockProjectStore_Get_NotFound(t *testing.T) {
	s := NewMockProjectStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockProjectStore_GetBySlug(t *testing.T) {
	s := NewMockProjectStore()
	companyID := uuid.New()
	p := makeProject(companyID, "slug-proj")
	_ = s.Create(ctx(), p)
	got, err := s.GetBySlug(ctx(), companyID, "slug-proj")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID {
		t.Fatal("ID mismatch")
	}
}

func TestMockProjectStore_GetBySlug_WrongCompany(t *testing.T) {
	s := NewMockProjectStore()
	_ = s.Create(ctx(), makeProject(uuid.New(), "scoped"))
	_, err := s.GetBySlug(ctx(), uuid.New(), "scoped")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong company, got %v", err)
	}
}

func TestMockProjectStore_Update(t *testing.T) {
	s := NewMockProjectStore()
	p := makeProject(uuid.New(), "up-proj")
	_ = s.Create(ctx(), p)
	p.Description = "updated"
	if err := s.Update(ctx(), p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx(), p.ID)
	if got.Description != "updated" {
		t.Fatal("description not updated")
	}
}

func TestMockProjectStore_Update_NotFound(t *testing.T) {
	s := NewMockProjectStore()
	p := makeProject(uuid.New(), "ghost")
	p.ID = uuid.New()
	if err := s.Update(ctx(), p); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockProjectStore_Delete(t *testing.T) {
	s := NewMockProjectStore()
	p := makeProject(uuid.New(), "del-proj")
	_ = s.Create(ctx(), p)
	if err := s.Delete(ctx(), p.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), p.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}

func TestMockProjectStore_Delete_NotFound(t *testing.T) {
	s := NewMockProjectStore()
	if err := s.Delete(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockProjectStore_List(t *testing.T) {
	s := NewMockProjectStore()
	cid := uuid.New()
	_ = s.Create(ctx(), makeProject(cid, "p1"))
	_ = s.Create(ctx(), makeProject(cid, "p2"))
	_ = s.Create(ctx(), makeProject(uuid.New(), "p3"))
	list, _ := s.List(ctx(), ProjectFilter{})
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
}

func TestMockProjectStore_List_FilterByCompanyID(t *testing.T) {
	s := NewMockProjectStore()
	cid := uuid.New()
	_ = s.Create(ctx(), makeProject(cid, "inside"))
	_ = s.Create(ctx(), makeProject(uuid.New(), "outside"))
	list, _ := s.List(ctx(), ProjectFilter{CompanyID: &cid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockProjectStore_ListForUser(t *testing.T) {
	ps := NewMockProjectStore()
	ms := NewMockMembershipStore()
	ps.SetMembershipStore(ms)
	userID := uuid.New()
	companyID := uuid.New()
	p := makeProject(companyID, "user-proj")
	_ = ps.Create(ctx(), p)
	pid := p.ID
	_ = ms.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, ProjectID: &pid, Role: RoleEditor})
	list, _ := ps.ListForUser(ctx(), userID)
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockProjectStore_ListForUser_NoMembership(t *testing.T) {
	ps := NewMockProjectStore()
	ms := NewMockMembershipStore()
	ps.SetMembershipStore(ms)
	_ = ps.Create(ctx(), makeProject(uuid.New(), "no-access"))
	list, _ := ps.ListForUser(ctx(), uuid.New())
	if len(list) != 0 {
		t.Fatalf("expected 0, got %d", len(list))
	}
}

// ===========================================================================
// WorkflowStore Tests
// ===========================================================================

func TestMockWorkflowStore_Create(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "wf1")
	if err := s.Create(ctx(), w); err != nil {
		t.Fatal(err)
	}
	if w.Version != 1 {
		t.Fatalf("expected version 1, got %d", w.Version)
	}
}

func TestMockWorkflowStore_Create_DuplicateSlug(t *testing.T) {
	s := NewMockWorkflowStore()
	pid := uuid.New()
	_ = s.Create(ctx(), makeWorkflow(pid, "dup"))
	err := s.Create(ctx(), makeWorkflow(pid, "dup"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockWorkflowStore_Create_SameSlugDifferentProject(t *testing.T) {
	s := NewMockWorkflowStore()
	_ = s.Create(ctx(), makeWorkflow(uuid.New(), "same"))
	if err := s.Create(ctx(), makeWorkflow(uuid.New(), "same")); err != nil {
		t.Fatalf("expected no error for different project, got %v", err)
	}
}

func TestMockWorkflowStore_Get(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "get-wf")
	_ = s.Create(ctx(), w)
	got, err := s.Get(ctx(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "get-wf" {
		t.Fatal("slug mismatch")
	}
}

func TestMockWorkflowStore_Get_NotFound(t *testing.T) {
	s := NewMockWorkflowStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockWorkflowStore_GetBySlug(t *testing.T) {
	s := NewMockWorkflowStore()
	pid := uuid.New()
	w := makeWorkflow(pid, "slug-wf")
	_ = s.Create(ctx(), w)
	got, err := s.GetBySlug(ctx(), pid, "slug-wf")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != w.ID {
		t.Fatal("ID mismatch")
	}
}

func TestMockWorkflowStore_GetBySlug_NotFound(t *testing.T) {
	s := NewMockWorkflowStore()
	_, err := s.GetBySlug(ctx(), uuid.New(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockWorkflowStore_Update_VersionIncrement(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "ver-wf")
	_ = s.Create(ctx(), w)
	if w.Version != 1 {
		t.Fatalf("initial version should be 1, got %d", w.Version)
	}
	w.ConfigYAML = "version: 2"
	if err := s.Update(ctx(), w); err != nil {
		t.Fatal(err)
	}
	if w.Version != 2 {
		t.Fatalf("expected version 2, got %d", w.Version)
	}
	w.ConfigYAML = "version: 3"
	_ = s.Update(ctx(), w)
	if w.Version != 3 {
		t.Fatalf("expected version 3, got %d", w.Version)
	}
}

func TestMockWorkflowStore_Update_NotFound(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "ghost")
	w.ID = uuid.New()
	if err := s.Update(ctx(), w); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockWorkflowStore_Update_StatusTransition(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "status-wf")
	_ = s.Create(ctx(), w)
	w.Status = WorkflowStatusActive
	if err := s.Update(ctx(), w); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx(), w.ID)
	if got.Status != WorkflowStatusActive {
		t.Fatal("status not updated")
	}
}

func TestMockWorkflowStore_Delete(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "del-wf")
	_ = s.Create(ctx(), w)
	if err := s.Delete(ctx(), w.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), w.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found after delete")
	}
}

func TestMockWorkflowStore_Delete_NotFound(t *testing.T) {
	s := NewMockWorkflowStore()
	if err := s.Delete(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockWorkflowStore_Delete_RemovesVersionHistory(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "del-ver")
	_ = s.Create(ctx(), w)
	wid := w.ID
	w.ConfigYAML = "v2"
	_ = s.Update(ctx(), w)
	_ = s.Delete(ctx(), wid)
	_, err := s.ListVersions(ctx(), wid)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected version history to be cleaned up")
	}
}

func TestMockWorkflowStore_List(t *testing.T) {
	s := NewMockWorkflowStore()
	pid := uuid.New()
	_ = s.Create(ctx(), makeWorkflow(pid, "w1"))
	_ = s.Create(ctx(), makeWorkflow(pid, "w2"))
	_ = s.Create(ctx(), makeWorkflow(uuid.New(), "w3"))
	list, _ := s.List(ctx(), WorkflowFilter{})
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
}

func TestMockWorkflowStore_List_FilterByProjectID(t *testing.T) {
	s := NewMockWorkflowStore()
	pid := uuid.New()
	_ = s.Create(ctx(), makeWorkflow(pid, "mine"))
	_ = s.Create(ctx(), makeWorkflow(uuid.New(), "other"))
	list, _ := s.List(ctx(), WorkflowFilter{ProjectID: &pid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockWorkflowStore_List_FilterByStatus(t *testing.T) {
	s := NewMockWorkflowStore()
	pid := uuid.New()
	w1 := makeWorkflow(pid, "draft-wf")
	w2 := makeWorkflow(pid, "active-wf")
	_ = s.Create(ctx(), w1)
	_ = s.Create(ctx(), w2)
	w2.Status = WorkflowStatusActive
	_ = s.Update(ctx(), w2)
	list, _ := s.List(ctx(), WorkflowFilter{Status: WorkflowStatusActive})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockWorkflowStore_GetVersion(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "ver")
	_ = s.Create(ctx(), w)
	w.ConfigYAML = "v2-yaml"
	_ = s.Update(ctx(), w)
	v1, err := s.GetVersion(ctx(), w.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.ConfigYAML != "version: 1" {
		t.Fatalf("v1 yaml mismatch: %s", v1.ConfigYAML)
	}
	v2, err := s.GetVersion(ctx(), w.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if v2.ConfigYAML != "v2-yaml" {
		t.Fatalf("v2 yaml mismatch: %s", v2.ConfigYAML)
	}
}

func TestMockWorkflowStore_GetVersion_NotFound(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "ver-nf")
	_ = s.Create(ctx(), w)
	_, err := s.GetVersion(ctx(), w.ID, 99)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockWorkflowStore_GetVersion_UnknownWorkflow(t *testing.T) {
	s := NewMockWorkflowStore()
	_, err := s.GetVersion(ctx(), uuid.New(), 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockWorkflowStore_ListVersions(t *testing.T) {
	s := NewMockWorkflowStore()
	w := makeWorkflow(uuid.New(), "lv")
	_ = s.Create(ctx(), w)
	w.ConfigYAML = "v2"
	_ = s.Update(ctx(), w)
	w.ConfigYAML = "v3"
	_ = s.Update(ctx(), w)
	versions, err := s.ListVersions(ctx(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0].Version != 1 || versions[1].Version != 2 || versions[2].Version != 3 {
		t.Fatal("versions not ordered correctly")
	}
}

func TestMockWorkflowStore_ListVersions_NotFound(t *testing.T) {
	s := NewMockWorkflowStore()
	_, err := s.ListVersions(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ===========================================================================
// MembershipStore Tests
// ===========================================================================

func TestMockMembershipStore_Create_CompanyLevel(t *testing.T) {
	s := NewMockMembershipStore()
	m := &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleAdmin}
	if err := s.Create(ctx(), m); err != nil {
		t.Fatal(err)
	}
	if m.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
	if m.ProjectID != nil {
		t.Fatal("expected nil ProjectID for company-level")
	}
}

func TestMockMembershipStore_Create_ProjectLevel(t *testing.T) {
	s := NewMockMembershipStore()
	pid := uuid.New()
	m := &Membership{UserID: uuid.New(), CompanyID: uuid.New(), ProjectID: &pid, Role: RoleEditor}
	if err := s.Create(ctx(), m); err != nil {
		t.Fatal(err)
	}
	if m.ProjectID == nil || *m.ProjectID != pid {
		t.Fatal("expected ProjectID to be set")
	}
}

func TestMockMembershipStore_Create_DuplicateCompanyLevel(t *testing.T) {
	s := NewMockMembershipStore()
	userID := uuid.New()
	companyID := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, Role: RoleViewer})
	err := s.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, Role: RoleAdmin})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockMembershipStore_Create_DuplicateProjectLevel(t *testing.T) {
	s := NewMockMembershipStore()
	userID := uuid.New()
	companyID := uuid.New()
	pid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, ProjectID: &pid, Role: RoleViewer})
	err := s.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, ProjectID: &pid, Role: RoleEditor})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestMockMembershipStore_Create_CompanyAndProjectSameUser(t *testing.T) {
	s := NewMockMembershipStore()
	userID := uuid.New()
	companyID := uuid.New()
	pid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, Role: RoleViewer})
	err := s.Create(ctx(), &Membership{UserID: userID, CompanyID: companyID, ProjectID: &pid, Role: RoleEditor})
	if err != nil {
		t.Fatalf("should allow both company and project membership, got %v", err)
	}
}

func TestMockMembershipStore_Get(t *testing.T) {
	s := NewMockMembershipStore()
	m := &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer}
	_ = s.Create(ctx(), m)
	got, err := s.Get(ctx(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != RoleViewer {
		t.Fatal("role mismatch")
	}
}

func TestMockMembershipStore_Get_NotFound(t *testing.T) {
	s := NewMockMembershipStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockMembershipStore_Update(t *testing.T) {
	s := NewMockMembershipStore()
	m := &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer}
	_ = s.Create(ctx(), m)
	m.Role = RoleAdmin
	if err := s.Update(ctx(), m); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx(), m.ID)
	if got.Role != RoleAdmin {
		t.Fatal("role not updated")
	}
}

func TestMockMembershipStore_Update_NotFound(t *testing.T) {
	s := NewMockMembershipStore()
	m := &Membership{ID: uuid.New(), UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer}
	if err := s.Update(ctx(), m); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockMembershipStore_Delete(t *testing.T) {
	s := NewMockMembershipStore()
	m := &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer}
	_ = s.Create(ctx(), m)
	if err := s.Delete(ctx(), m.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), m.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found after delete")
	}
}

func TestMockMembershipStore_Delete_NotFound(t *testing.T) {
	s := NewMockMembershipStore()
	if err := s.Delete(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockMembershipStore_List(t *testing.T) {
	s := NewMockMembershipStore()
	cid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: cid, Role: RoleViewer})
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: cid, Role: RoleAdmin})
	list, _ := s.List(ctx(), MembershipFilter{})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockMembershipStore_List_FilterByUserID(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: uuid.New(), Role: RoleViewer})
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer})
	list, _ := s.List(ctx(), MembershipFilter{UserID: &uid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockMembershipStore_List_FilterByCompanyID(t *testing.T) {
	s := NewMockMembershipStore()
	cid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: cid, Role: RoleViewer})
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer})
	list, _ := s.List(ctx(), MembershipFilter{CompanyID: &cid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockMembershipStore_List_FilterByProjectID(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	cid := uuid.New()
	pid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, Role: RoleViewer})
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, ProjectID: &pid, Role: RoleEditor})
	list, _ := s.List(ctx(), MembershipFilter{ProjectID: &pid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockMembershipStore_List_FilterByRole(t *testing.T) {
	s := NewMockMembershipStore()
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleViewer})
	_ = s.Create(ctx(), &Membership{UserID: uuid.New(), CompanyID: uuid.New(), Role: RoleAdmin})
	list, _ := s.List(ctx(), MembershipFilter{Role: RoleAdmin})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockMembershipStore_GetEffectiveRole_ProjectLevel(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	cid := uuid.New()
	pid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, Role: RoleViewer})
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, ProjectID: &pid, Role: RoleAdmin})
	role, err := s.GetEffectiveRole(ctx(), uid, cid, &pid)
	if err != nil {
		t.Fatal(err)
	}
	if role != RoleAdmin {
		t.Fatalf("expected admin (project-level), got %s", role)
	}
}

func TestMockMembershipStore_GetEffectiveRole_CascadeToCompany(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	cid := uuid.New()
	pid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, Role: RoleEditor})
	role, err := s.GetEffectiveRole(ctx(), uid, cid, &pid)
	if err != nil {
		t.Fatal(err)
	}
	if role != RoleEditor {
		t.Fatalf("expected editor (cascaded from company), got %s", role)
	}
}

func TestMockMembershipStore_GetEffectiveRole_CompanyLevelDirect(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	cid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, Role: RoleOwner})
	role, err := s.GetEffectiveRole(ctx(), uid, cid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if role != RoleOwner {
		t.Fatalf("expected owner, got %s", role)
	}
}

func TestMockMembershipStore_GetEffectiveRole_NotFound(t *testing.T) {
	s := NewMockMembershipStore()
	_, err := s.GetEffectiveRole(ctx(), uuid.New(), uuid.New(), new(uuid.New()))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockMembershipStore_GetEffectiveRole_NilProjectID(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	cid := uuid.New()
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, Role: RoleViewer})
	role, err := s.GetEffectiveRole(ctx(), uid, cid, nil)
	if err != nil {
		t.Fatal(err)
	}
	if role != RoleViewer {
		t.Fatalf("expected viewer, got %s", role)
	}
}

func TestMockMembershipStore_GetEffectiveRole_NilProjectID_NotFound(t *testing.T) {
	s := NewMockMembershipStore()
	uid := uuid.New()
	cid := uuid.New()
	pid := uuid.New()
	// Only project-level membership exists.
	_ = s.Create(ctx(), &Membership{UserID: uid, CompanyID: cid, ProjectID: &pid, Role: RoleAdmin})
	_, err := s.GetEffectiveRole(ctx(), uid, cid, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound when only project membership exists, got %v", err)
	}
}

// ===========================================================================
// CrossWorkflowLinkStore Tests
// ===========================================================================

func TestMockCrossWorkflowLinkStore_Create(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	l := &CrossWorkflowLink{
		SourceWorkflowID: uuid.New(),
		TargetWorkflowID: uuid.New(),
		LinkType:         "dependency",
		CreatedBy:        uuid.New(),
	}
	if err := s.Create(ctx(), l); err != nil {
		t.Fatal(err)
	}
	if l.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockCrossWorkflowLinkStore_Get(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	l := &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "dep", CreatedBy: uuid.New()}
	_ = s.Create(ctx(), l)
	got, err := s.Get(ctx(), l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LinkType != "dep" {
		t.Fatal("linktype mismatch")
	}
}

func TestMockCrossWorkflowLinkStore_Get_NotFound(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockCrossWorkflowLinkStore_Delete(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	l := &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "del", CreatedBy: uuid.New()}
	_ = s.Create(ctx(), l)
	if err := s.Delete(ctx(), l.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), l.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found after delete")
	}
}

func TestMockCrossWorkflowLinkStore_Delete_NotFound(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	if err := s.Delete(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockCrossWorkflowLinkStore_List_BySource(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	src := uuid.New()
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: src, TargetWorkflowID: uuid.New(), LinkType: "a", CreatedBy: uuid.New()})
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: src, TargetWorkflowID: uuid.New(), LinkType: "b", CreatedBy: uuid.New()})
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "c", CreatedBy: uuid.New()})
	list, _ := s.List(ctx(), CrossWorkflowLinkFilter{SourceWorkflowID: &src})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockCrossWorkflowLinkStore_List_ByTarget(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	tgt := uuid.New()
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: tgt, LinkType: "a", CreatedBy: uuid.New()})
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "b", CreatedBy: uuid.New()})
	list, _ := s.List(ctx(), CrossWorkflowLinkFilter{TargetWorkflowID: &tgt})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockCrossWorkflowLinkStore_List_ByLinkType(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "dependency", CreatedBy: uuid.New()})
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "trigger", CreatedBy: uuid.New()})
	list, _ := s.List(ctx(), CrossWorkflowLinkFilter{LinkType: "dependency"})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockCrossWorkflowLinkStore_List_All(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "a", CreatedBy: uuid.New()})
	_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "b", CreatedBy: uuid.New()})
	list, _ := s.List(ctx(), CrossWorkflowLinkFilter{})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockCrossWorkflowLinkStore_List_Pagination(t *testing.T) {
	s := NewMockCrossWorkflowLinkStore()
	for range 5 {
		_ = s.Create(ctx(), &CrossWorkflowLink{SourceWorkflowID: uuid.New(), TargetWorkflowID: uuid.New(), LinkType: "x", CreatedBy: uuid.New()})
	}
	list, _ := s.List(ctx(), CrossWorkflowLinkFilter{Pagination: Pagination{Offset: 1, Limit: 2}})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

// ===========================================================================
// SessionStore Tests
// ===========================================================================

func TestMockSessionStore_Create(t *testing.T) {
	s := NewMockSessionStore()
	sess := &Session{UserID: uuid.New(), Token: "tok1", Active: true, ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.Create(ctx(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockSessionStore_Get(t *testing.T) {
	s := NewMockSessionStore()
	sess := &Session{UserID: uuid.New(), Token: "tok2", Active: true, ExpiresAt: time.Now().Add(time.Hour)}
	_ = s.Create(ctx(), sess)
	got, err := s.Get(ctx(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != "tok2" {
		t.Fatal("token mismatch")
	}
}

func TestMockSessionStore_Get_NotFound(t *testing.T) {
	s := NewMockSessionStore()
	_, err := s.Get(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockSessionStore_GetByToken(t *testing.T) {
	s := NewMockSessionStore()
	sess := &Session{UserID: uuid.New(), Token: "find-me", Active: true, ExpiresAt: time.Now().Add(time.Hour)}
	_ = s.Create(ctx(), sess)
	got, err := s.GetByToken(ctx(), "find-me")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sess.ID {
		t.Fatal("ID mismatch")
	}
}

func TestMockSessionStore_GetByToken_NotFound(t *testing.T) {
	s := NewMockSessionStore()
	_, err := s.GetByToken(ctx(), "missing-token")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockSessionStore_Update(t *testing.T) {
	s := NewMockSessionStore()
	sess := &Session{UserID: uuid.New(), Token: "tok-up", Active: true, ExpiresAt: time.Now().Add(time.Hour)}
	_ = s.Create(ctx(), sess)
	sess.Active = false
	if err := s.Update(ctx(), sess); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx(), sess.ID)
	if got.Active {
		t.Fatal("expected inactive")
	}
}

func TestMockSessionStore_Update_NotFound(t *testing.T) {
	s := NewMockSessionStore()
	sess := &Session{ID: uuid.New(), UserID: uuid.New(), Token: "nope"}
	if err := s.Update(ctx(), sess); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockSessionStore_Delete(t *testing.T) {
	s := NewMockSessionStore()
	sess := &Session{UserID: uuid.New(), Token: "del-tok", Active: true, ExpiresAt: time.Now().Add(time.Hour)}
	_ = s.Create(ctx(), sess)
	if err := s.Delete(ctx(), sess.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(ctx(), sess.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}

func TestMockSessionStore_Delete_NotFound(t *testing.T) {
	s := NewMockSessionStore()
	if err := s.Delete(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockSessionStore_List_FilterByUserID(t *testing.T) {
	s := NewMockSessionStore()
	uid := uuid.New()
	_ = s.Create(ctx(), &Session{UserID: uid, Token: "a", Active: true, ExpiresAt: time.Now().Add(time.Hour)})
	_ = s.Create(ctx(), &Session{UserID: uuid.New(), Token: "b", Active: true, ExpiresAt: time.Now().Add(time.Hour)})
	list, _ := s.List(ctx(), SessionFilter{UserID: &uid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockSessionStore_DeleteExpired(t *testing.T) {
	s := NewMockSessionStore()
	_ = s.Create(ctx(), &Session{UserID: uuid.New(), Token: "expired1", Active: true, ExpiresAt: time.Now().Add(-time.Hour)})
	_ = s.Create(ctx(), &Session{UserID: uuid.New(), Token: "expired2", Active: true, ExpiresAt: time.Now().Add(-time.Minute)})
	_ = s.Create(ctx(), &Session{UserID: uuid.New(), Token: "valid", Active: true, ExpiresAt: time.Now().Add(time.Hour)})
	count, err := s.DeleteExpired(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 expired deleted, got %d", count)
	}
	list, _ := s.List(ctx(), SessionFilter{})
	if len(list) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(list))
	}
}

// ===========================================================================
// ExecutionStore Tests
// ===========================================================================

func TestMockExecutionStore_CreateExecution(t *testing.T) {
	s := NewMockExecutionStore()
	e := &WorkflowExecution{WorkflowID: uuid.New(), TriggerType: "http", Status: ExecutionStatusPending}
	if err := s.CreateExecution(ctx(), e); err != nil {
		t.Fatal(err)
	}
	if e.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockExecutionStore_GetExecution(t *testing.T) {
	s := NewMockExecutionStore()
	e := &WorkflowExecution{WorkflowID: uuid.New(), TriggerType: "http", Status: ExecutionStatusRunning}
	_ = s.CreateExecution(ctx(), e)
	got, err := s.GetExecution(ctx(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ExecutionStatusRunning {
		t.Fatal("status mismatch")
	}
}

func TestMockExecutionStore_GetExecution_NotFound(t *testing.T) {
	s := NewMockExecutionStore()
	_, err := s.GetExecution(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockExecutionStore_UpdateExecution(t *testing.T) {
	s := NewMockExecutionStore()
	e := &WorkflowExecution{WorkflowID: uuid.New(), TriggerType: "http", Status: ExecutionStatusPending}
	_ = s.CreateExecution(ctx(), e)
	e.Status = ExecutionStatusCompleted
	if err := s.UpdateExecution(ctx(), e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetExecution(ctx(), e.ID)
	if got.Status != ExecutionStatusCompleted {
		t.Fatal("status not updated")
	}
}

func TestMockExecutionStore_UpdateExecution_NotFound(t *testing.T) {
	s := NewMockExecutionStore()
	e := &WorkflowExecution{ID: uuid.New(), WorkflowID: uuid.New()}
	if err := s.UpdateExecution(ctx(), e); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockExecutionStore_ListExecutions(t *testing.T) {
	s := NewMockExecutionStore()
	wid := uuid.New()
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, TriggerType: "http", Status: ExecutionStatusPending})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, TriggerType: "cron", Status: ExecutionStatusCompleted})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: uuid.New(), TriggerType: "http", Status: ExecutionStatusPending})
	list, _ := s.ListExecutions(ctx(), ExecutionFilter{WorkflowID: &wid})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockExecutionStore_ListExecutions_FilterByStatus(t *testing.T) {
	s := NewMockExecutionStore()
	wid := uuid.New()
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusPending})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusCompleted})
	list, _ := s.ListExecutions(ctx(), ExecutionFilter{Status: ExecutionStatusPending})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockExecutionStore_ListExecutions_FilterBySince(t *testing.T) {
	s := NewMockExecutionStore()
	wid := uuid.New()
	e1 := &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusPending}
	_ = s.CreateExecution(ctx(), e1)
	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(10 * time.Millisecond)
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusRunning})
	list, _ := s.ListExecutions(ctx(), ExecutionFilter{Since: &cutoff})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockExecutionStore_ListExecutions_FilterByUntil(t *testing.T) {
	s := NewMockExecutionStore()
	wid := uuid.New()
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusPending})
	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(10 * time.Millisecond)
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusRunning})
	list, _ := s.ListExecutions(ctx(), ExecutionFilter{Until: &cutoff})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockExecutionStore_ListExecutions_Pagination(t *testing.T) {
	s := NewMockExecutionStore()
	for range 5 {
		_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: uuid.New(), Status: ExecutionStatusPending})
	}
	list, _ := s.ListExecutions(ctx(), ExecutionFilter{Pagination: Pagination{Offset: 1, Limit: 2}})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockExecutionStore_CreateStep(t *testing.T) {
	s := NewMockExecutionStore()
	step := &ExecutionStep{ExecutionID: uuid.New(), StepName: "step1", StepType: "action", SequenceNum: 1, Status: StepStatusPending}
	if err := s.CreateStep(ctx(), step); err != nil {
		t.Fatal(err)
	}
	if step.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockExecutionStore_UpdateStep(t *testing.T) {
	s := NewMockExecutionStore()
	step := &ExecutionStep{ExecutionID: uuid.New(), StepName: "step1", StepType: "action", SequenceNum: 1, Status: StepStatusPending}
	_ = s.CreateStep(ctx(), step)
	step.Status = StepStatusCompleted
	if err := s.UpdateStep(ctx(), step); err != nil {
		t.Fatal(err)
	}
}

func TestMockExecutionStore_UpdateStep_NotFound(t *testing.T) {
	s := NewMockExecutionStore()
	step := &ExecutionStep{ID: uuid.New(), ExecutionID: uuid.New()}
	if err := s.UpdateStep(ctx(), step); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockExecutionStore_ListSteps_Ordered(t *testing.T) {
	s := NewMockExecutionStore()
	eid := uuid.New()
	_ = s.CreateStep(ctx(), &ExecutionStep{ExecutionID: eid, StepName: "third", SequenceNum: 3, Status: StepStatusPending})
	_ = s.CreateStep(ctx(), &ExecutionStep{ExecutionID: eid, StepName: "first", SequenceNum: 1, Status: StepStatusPending})
	_ = s.CreateStep(ctx(), &ExecutionStep{ExecutionID: eid, StepName: "second", SequenceNum: 2, Status: StepStatusPending})
	steps, err := s.ListSteps(ctx(), eid)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3, got %d", len(steps))
	}
	if steps[0].StepName != "first" || steps[1].StepName != "second" || steps[2].StepName != "third" {
		t.Fatal("steps not ordered by sequence_num")
	}
}

func TestMockExecutionStore_ListSteps_DifferentExecution(t *testing.T) {
	s := NewMockExecutionStore()
	eid1 := uuid.New()
	eid2 := uuid.New()
	_ = s.CreateStep(ctx(), &ExecutionStep{ExecutionID: eid1, StepName: "s1", SequenceNum: 1, Status: StepStatusPending})
	_ = s.CreateStep(ctx(), &ExecutionStep{ExecutionID: eid2, StepName: "s2", SequenceNum: 1, Status: StepStatusPending})
	steps, _ := s.ListSteps(ctx(), eid1)
	if len(steps) != 1 {
		t.Fatalf("expected 1, got %d", len(steps))
	}
}

func TestMockExecutionStore_CountByStatus(t *testing.T) {
	s := NewMockExecutionStore()
	wid := uuid.New()
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusPending})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusPending})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusCompleted})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: wid, Status: ExecutionStatusFailed})
	_ = s.CreateExecution(ctx(), &WorkflowExecution{WorkflowID: uuid.New(), Status: ExecutionStatusPending})
	counts, err := s.CountByStatus(ctx(), wid)
	if err != nil {
		t.Fatal(err)
	}
	if counts[ExecutionStatusPending] != 2 {
		t.Fatalf("expected 2 pending, got %d", counts[ExecutionStatusPending])
	}
	if counts[ExecutionStatusCompleted] != 1 {
		t.Fatalf("expected 1 completed, got %d", counts[ExecutionStatusCompleted])
	}
	if counts[ExecutionStatusFailed] != 1 {
		t.Fatalf("expected 1 failed, got %d", counts[ExecutionStatusFailed])
	}
}

func TestMockExecutionStore_CountByStatus_Empty(t *testing.T) {
	s := NewMockExecutionStore()
	counts, err := s.CountByStatus(ctx(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty map, got %v", counts)
	}
}

func TestMockExecutionStore_MultipleExecutionsSameWorkflow(t *testing.T) {
	s := NewMockExecutionStore()
	wid := uuid.New()
	e1 := &WorkflowExecution{WorkflowID: wid, TriggerType: "http", Status: ExecutionStatusPending}
	e2 := &WorkflowExecution{WorkflowID: wid, TriggerType: "cron", Status: ExecutionStatusRunning}
	_ = s.CreateExecution(ctx(), e1)
	_ = s.CreateExecution(ctx(), e2)
	list, _ := s.ListExecutions(ctx(), ExecutionFilter{WorkflowID: &wid})
	if len(list) != 2 {
		t.Fatalf("expected 2 executions for same workflow, got %d", len(list))
	}
}

// ===========================================================================
// LogStore Tests
// ===========================================================================

func TestMockLogStore_Append(t *testing.T) {
	s := NewMockLogStore()
	l := &ExecutionLog{WorkflowID: uuid.New(), Level: LogLevelInfo, Message: "hello"}
	if err := s.Append(ctx(), l); err != nil {
		t.Fatal(err)
	}
	if l.ID == 0 {
		t.Fatal("expected auto-increment ID")
	}
}

func TestMockLogStore_Append_AutoIncrementID(t *testing.T) {
	s := NewMockLogStore()
	l1 := &ExecutionLog{WorkflowID: uuid.New(), Level: LogLevelInfo, Message: "first"}
	l2 := &ExecutionLog{WorkflowID: uuid.New(), Level: LogLevelInfo, Message: "second"}
	_ = s.Append(ctx(), l1)
	_ = s.Append(ctx(), l2)
	if l2.ID <= l1.ID {
		t.Fatalf("expected l2.ID > l1.ID, got %d <= %d", l2.ID, l1.ID)
	}
}

func TestMockLogStore_Query(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "a"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelError, Message: "b"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: uuid.New(), Level: LogLevelInfo, Message: "c"})
	results, _ := s.Query(ctx(), LogFilter{WorkflowID: &wid})
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestMockLogStore_Query_FilterByLevel(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "info"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelError, Message: "error"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelWarn, Message: "warn"})
	results, _ := s.Query(ctx(), LogFilter{Level: LogLevelError})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockLogStore_Query_FilterByModule(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, ModuleName: "http", Message: "a"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, ModuleName: "cache", Message: "b"})
	results, _ := s.Query(ctx(), LogFilter{ModuleName: "http"})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockLogStore_Query_FilterByExecutionID(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	eid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, ExecutionID: &eid, Level: LogLevelInfo, Message: "with exec"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "no exec"})
	results, _ := s.Query(ctx(), LogFilter{ExecutionID: &eid})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockLogStore_Query_FilterByDateRange(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "early"})
	time.Sleep(10 * time.Millisecond)
	since := time.Now()
	time.Sleep(10 * time.Millisecond)
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "late"})
	results, _ := s.Query(ctx(), LogFilter{Since: &since})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockLogStore_Query_Pagination(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	for range 10 {
		_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "m"})
	}
	results, _ := s.Query(ctx(), LogFilter{Pagination: Pagination{Offset: 3, Limit: 4}})
	if len(results) != 4 {
		t.Fatalf("expected 4, got %d", len(results))
	}
}

func TestMockLogStore_CountByLevel(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "i1"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, Message: "i2"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelError, Message: "e1"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelWarn, Message: "w1"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: uuid.New(), Level: LogLevelInfo, Message: "other"})
	counts, err := s.CountByLevel(ctx(), wid)
	if err != nil {
		t.Fatal(err)
	}
	if counts[LogLevelInfo] != 2 {
		t.Fatalf("expected 2 info, got %d", counts[LogLevelInfo])
	}
	if counts[LogLevelError] != 1 {
		t.Fatalf("expected 1 error, got %d", counts[LogLevelError])
	}
	if counts[LogLevelWarn] != 1 {
		t.Fatalf("expected 1 warn, got %d", counts[LogLevelWarn])
	}
}

func TestMockLogStore_CountByLevel_Empty(t *testing.T) {
	s := NewMockLogStore()
	counts, err := s.CountByLevel(ctx(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected empty, got %v", counts)
	}
}

func TestMockLogStore_Query_CombinedFilters(t *testing.T) {
	s := NewMockLogStore()
	wid := uuid.New()
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, ModuleName: "http", Message: "a"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelError, ModuleName: "http", Message: "b"})
	_ = s.Append(ctx(), &ExecutionLog{WorkflowID: wid, Level: LogLevelInfo, ModuleName: "cache", Message: "c"})
	results, _ := s.Query(ctx(), LogFilter{WorkflowID: &wid, Level: LogLevelInfo, ModuleName: "http"})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

// ===========================================================================
// AuditStore Tests
// ===========================================================================

func TestMockAuditStore_Record(t *testing.T) {
	s := NewMockAuditStore()
	uid := uuid.New()
	e := &AuditEntry{UserID: &uid, Action: "create", ResourceType: "workflow"}
	if err := s.Record(ctx(), e); err != nil {
		t.Fatal(err)
	}
	if e.ID == 0 {
		t.Fatal("expected auto-increment ID")
	}
}

func TestMockAuditStore_Record_AutoIncrementID(t *testing.T) {
	s := NewMockAuditStore()
	e1 := &AuditEntry{Action: "a", ResourceType: "x"}
	e2 := &AuditEntry{Action: "b", ResourceType: "y"}
	_ = s.Record(ctx(), e1)
	_ = s.Record(ctx(), e2)
	if e2.ID <= e1.ID {
		t.Fatalf("expected e2.ID > e1.ID, got %d <= %d", e2.ID, e1.ID)
	}
}

func TestMockAuditStore_Query(t *testing.T) {
	s := NewMockAuditStore()
	_ = s.Record(ctx(), &AuditEntry{Action: "create", ResourceType: "workflow"})
	_ = s.Record(ctx(), &AuditEntry{Action: "delete", ResourceType: "workflow"})
	results, _ := s.Query(ctx(), AuditFilter{})
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestMockAuditStore_Query_FilterByUser(t *testing.T) {
	s := NewMockAuditStore()
	uid := uuid.New()
	_ = s.Record(ctx(), &AuditEntry{UserID: &uid, Action: "create", ResourceType: "wf"})
	_ = s.Record(ctx(), &AuditEntry{Action: "delete", ResourceType: "wf"})
	results, _ := s.Query(ctx(), AuditFilter{UserID: &uid})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockAuditStore_Query_FilterByAction(t *testing.T) {
	s := NewMockAuditStore()
	_ = s.Record(ctx(), &AuditEntry{Action: "create", ResourceType: "wf"})
	_ = s.Record(ctx(), &AuditEntry{Action: "delete", ResourceType: "wf"})
	_ = s.Record(ctx(), &AuditEntry{Action: "create", ResourceType: "proj"})
	results, _ := s.Query(ctx(), AuditFilter{Action: "create"})
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestMockAuditStore_Query_FilterByResourceType(t *testing.T) {
	s := NewMockAuditStore()
	_ = s.Record(ctx(), &AuditEntry{Action: "create", ResourceType: "workflow"})
	_ = s.Record(ctx(), &AuditEntry{Action: "create", ResourceType: "project"})
	results, _ := s.Query(ctx(), AuditFilter{ResourceType: "workflow"})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockAuditStore_Query_FilterByResourceID(t *testing.T) {
	s := NewMockAuditStore()
	rid := uuid.New()
	_ = s.Record(ctx(), &AuditEntry{Action: "update", ResourceType: "wf", ResourceID: &rid})
	_ = s.Record(ctx(), &AuditEntry{Action: "update", ResourceType: "wf"})
	results, _ := s.Query(ctx(), AuditFilter{ResourceID: &rid})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockAuditStore_Query_FilterByDateRange(t *testing.T) {
	s := NewMockAuditStore()
	_ = s.Record(ctx(), &AuditEntry{Action: "early", ResourceType: "x"})
	time.Sleep(10 * time.Millisecond)
	since := time.Now()
	time.Sleep(10 * time.Millisecond)
	_ = s.Record(ctx(), &AuditEntry{Action: "late", ResourceType: "x"})
	results, _ := s.Query(ctx(), AuditFilter{Since: &since})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockAuditStore_Query_Pagination(t *testing.T) {
	s := NewMockAuditStore()
	for range 8 {
		_ = s.Record(ctx(), &AuditEntry{Action: "test", ResourceType: "x"})
	}
	results, _ := s.Query(ctx(), AuditFilter{Pagination: Pagination{Offset: 2, Limit: 3}})
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
}

func TestMockAuditStore_Query_CombinedFilters(t *testing.T) {
	s := NewMockAuditStore()
	uid := uuid.New()
	_ = s.Record(ctx(), &AuditEntry{UserID: &uid, Action: "create", ResourceType: "workflow"})
	_ = s.Record(ctx(), &AuditEntry{UserID: &uid, Action: "delete", ResourceType: "workflow"})
	_ = s.Record(ctx(), &AuditEntry{Action: "create", ResourceType: "workflow"})
	results, _ := s.Query(ctx(), AuditFilter{UserID: &uid, Action: "create"})
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestMockAuditStore_Record_NilUserID(t *testing.T) {
	s := NewMockAuditStore()
	e := &AuditEntry{Action: "system", ResourceType: "config"}
	if err := s.Record(ctx(), e); err != nil {
		t.Fatal(err)
	}
	results, _ := s.Query(ctx(), AuditFilter{})
	if results[0].UserID != nil {
		t.Fatal("expected nil UserID")
	}
}

// ===========================================================================
// IAMStore Tests
// ===========================================================================

func TestMockIAMStore_CreateProvider(t *testing.T) {
	s := NewMockIAMStore()
	p := &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderOIDC, Name: "test-oidc", Enabled: true}
	if err := s.CreateProvider(ctx(), p); err != nil {
		t.Fatal(err)
	}
	if p.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockIAMStore_GetProvider(t *testing.T) {
	s := NewMockIAMStore()
	p := &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderAWS, Name: "aws", Enabled: true}
	_ = s.CreateProvider(ctx(), p)
	got, err := s.GetProvider(ctx(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "aws" {
		t.Fatal("name mismatch")
	}
}

func TestMockIAMStore_GetProvider_NotFound(t *testing.T) {
	s := NewMockIAMStore()
	_, err := s.GetProvider(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockIAMStore_UpdateProvider(t *testing.T) {
	s := NewMockIAMStore()
	p := &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderOIDC, Name: "before", Enabled: true}
	_ = s.CreateProvider(ctx(), p)
	p.Name = "after"
	if err := s.UpdateProvider(ctx(), p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProvider(ctx(), p.ID)
	if got.Name != "after" {
		t.Fatal("name not updated")
	}
}

func TestMockIAMStore_UpdateProvider_NotFound(t *testing.T) {
	s := NewMockIAMStore()
	p := &IAMProviderConfig{ID: uuid.New()}
	if err := s.UpdateProvider(ctx(), p); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockIAMStore_DeleteProvider(t *testing.T) {
	s := NewMockIAMStore()
	p := &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderLDAP, Name: "ldap", Enabled: true}
	_ = s.CreateProvider(ctx(), p)
	if err := s.DeleteProvider(ctx(), p.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetProvider(ctx(), p.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}

func TestMockIAMStore_DeleteProvider_NotFound(t *testing.T) {
	s := NewMockIAMStore()
	if err := s.DeleteProvider(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockIAMStore_ListProviders(t *testing.T) {
	s := NewMockIAMStore()
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderOIDC, Name: "a", Enabled: true})
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderAWS, Name: "b", Enabled: false})
	list, _ := s.ListProviders(ctx(), IAMProviderFilter{})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockIAMStore_ListProviders_FilterByCompanyID(t *testing.T) {
	s := NewMockIAMStore()
	cid := uuid.New()
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: cid, ProviderType: IAMProviderOIDC, Name: "mine", Enabled: true})
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderOIDC, Name: "other", Enabled: true})
	list, _ := s.ListProviders(ctx(), IAMProviderFilter{CompanyID: &cid})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockIAMStore_ListProviders_FilterByEnabled(t *testing.T) {
	s := NewMockIAMStore()
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderOIDC, Name: "on", Enabled: true})
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderAWS, Name: "off", Enabled: false})
	list, _ := s.ListProviders(ctx(), IAMProviderFilter{Enabled: new(true)})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Name != "on" {
		t.Fatal("expected enabled provider")
	}
}

func TestMockIAMStore_ListProviders_FilterByProviderType(t *testing.T) {
	s := NewMockIAMStore()
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderOIDC, Name: "oidc", Enabled: true})
	_ = s.CreateProvider(ctx(), &IAMProviderConfig{CompanyID: uuid.New(), ProviderType: IAMProviderAWS, Name: "aws", Enabled: true})
	list, _ := s.ListProviders(ctx(), IAMProviderFilter{ProviderType: IAMProviderOIDC})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockIAMStore_CreateMapping(t *testing.T) {
	s := NewMockIAMStore()
	m := &IAMRoleMapping{
		ProviderID:         uuid.New(),
		ExternalIdentifier: "arn:aws:iam::123:role/admin",
		ResourceType:       "company",
		ResourceID:         uuid.New(),
		Role:               RoleAdmin,
	}
	if err := s.CreateMapping(ctx(), m); err != nil {
		t.Fatal(err)
	}
	if m.ID == uuid.Nil {
		t.Fatal("expected ID")
	}
}

func TestMockIAMStore_GetMapping(t *testing.T) {
	s := NewMockIAMStore()
	m := &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "ext1", ResourceType: "project", ResourceID: uuid.New(), Role: RoleEditor}
	_ = s.CreateMapping(ctx(), m)
	got, err := s.GetMapping(ctx(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExternalIdentifier != "ext1" {
		t.Fatal("external id mismatch")
	}
}

func TestMockIAMStore_GetMapping_NotFound(t *testing.T) {
	s := NewMockIAMStore()
	_, err := s.GetMapping(ctx(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockIAMStore_DeleteMapping(t *testing.T) {
	s := NewMockIAMStore()
	m := &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "del-ext", ResourceType: "company", ResourceID: uuid.New(), Role: RoleViewer}
	_ = s.CreateMapping(ctx(), m)
	if err := s.DeleteMapping(ctx(), m.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetMapping(ctx(), m.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("expected not found")
	}
}

func TestMockIAMStore_DeleteMapping_NotFound(t *testing.T) {
	s := NewMockIAMStore()
	if err := s.DeleteMapping(ctx(), uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockIAMStore_ListMappings(t *testing.T) {
	s := NewMockIAMStore()
	pid := uuid.New()
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: pid, ExternalIdentifier: "a", ResourceType: "company", ResourceID: uuid.New(), Role: RoleAdmin})
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: pid, ExternalIdentifier: "b", ResourceType: "project", ResourceID: uuid.New(), Role: RoleEditor})
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "c", ResourceType: "company", ResourceID: uuid.New(), Role: RoleViewer})
	list, _ := s.ListMappings(ctx(), IAMRoleMappingFilter{ProviderID: &pid})
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestMockIAMStore_ListMappings_FilterByResourceType(t *testing.T) {
	s := NewMockIAMStore()
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "a", ResourceType: "company", ResourceID: uuid.New(), Role: RoleAdmin})
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "b", ResourceType: "project", ResourceID: uuid.New(), Role: RoleEditor})
	list, _ := s.ListMappings(ctx(), IAMRoleMappingFilter{ResourceType: "company"})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockIAMStore_ListMappings_FilterByExternalIdentifier(t *testing.T) {
	s := NewMockIAMStore()
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "target-ext", ResourceType: "company", ResourceID: uuid.New(), Role: RoleAdmin})
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{ProviderID: uuid.New(), ExternalIdentifier: "other-ext", ResourceType: "company", ResourceID: uuid.New(), Role: RoleViewer})
	list, _ := s.ListMappings(ctx(), IAMRoleMappingFilter{ExternalIdentifier: "target-ext"})
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
}

func TestMockIAMStore_ResolveRole(t *testing.T) {
	s := NewMockIAMStore()
	provID := uuid.New()
	resID := uuid.New()
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{
		ProviderID:         provID,
		ExternalIdentifier: "arn:aws:iam::123:role/dev",
		ResourceType:       "project",
		ResourceID:         resID,
		Role:               RoleEditor,
	})
	role, err := s.ResolveRole(ctx(), provID, "arn:aws:iam::123:role/dev", "project", resID)
	if err != nil {
		t.Fatal(err)
	}
	if role != RoleEditor {
		t.Fatalf("expected editor, got %s", role)
	}
}

func TestMockIAMStore_ResolveRole_NotFound(t *testing.T) {
	s := NewMockIAMStore()
	_, err := s.ResolveRole(ctx(), uuid.New(), "unknown", "company", uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockIAMStore_ResolveRole_WrongProvider(t *testing.T) {
	s := NewMockIAMStore()
	provID := uuid.New()
	resID := uuid.New()
	_ = s.CreateMapping(ctx(), &IAMRoleMapping{
		ProviderID:         provID,
		ExternalIdentifier: "ext1",
		ResourceType:       "project",
		ResourceID:         resID,
		Role:               RoleAdmin,
	})
	_, err := s.ResolveRole(ctx(), uuid.New(), "ext1", "project", resID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong provider, got %v", err)
	}
}

// ===========================================================================
// Interface Compliance Tests
// ===========================================================================

func TestInterfaceCompliance(t *testing.T) {
	// These are compile-time checks via the var block in mock_stores.go,
	// but we test creation and basic usage here.
	tests := []struct {
		name string
		fn   func()
	}{
		{"UserStore", func() { var _ UserStore = NewMockUserStore() }},
		{"CompanyStore", func() { var _ CompanyStore = NewMockCompanyStore() }},
		{"ProjectStore", func() { var _ ProjectStore = NewMockProjectStore() }},
		{"WorkflowStore", func() { var _ WorkflowStore = NewMockWorkflowStore() }},
		{"MembershipStore", func() { var _ MembershipStore = NewMockMembershipStore() }},
		{"CrossWorkflowLinkStore", func() { var _ CrossWorkflowLinkStore = NewMockCrossWorkflowLinkStore() }},
		{"SessionStore", func() { var _ SessionStore = NewMockSessionStore() }},
		{"ExecutionStore", func() { var _ ExecutionStore = NewMockExecutionStore() }},
		{"LogStore", func() { var _ LogStore = NewMockLogStore() }},
		{"AuditStore", func() { var _ AuditStore = NewMockAuditStore() }},
		{"IAMStore", func() { var _ IAMStore = NewMockIAMStore() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn()
		})
	}
}
