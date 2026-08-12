package share

import (
	"errors"
	"testing"
)

func TestServiceCreateCollectionRequiresOwnedFilesAndRevokesOnDelete(t *testing.T) {
	repo := newTestRepository(t)
	storage := &fakeStorage{content: map[string][]byte{"uploads/document.txt": []byte("shared content")}}
	service := NewService(repo, storage)
	firstID := createTestFile(t, repo, 1, "active")
	secondID := createTestFile(t, repo, 1, "active")
	otherUserID := createTestFile(t, repo, 2, "active")

	if _, err := service.CreateCollection(1, []int64{firstID}, "", nil, nil); !errors.Is(err, ErrShareCollectionEmpty) {
		t.Fatalf("one-file collection error = %v, want %v", err, ErrShareCollectionEmpty)
	}
	if _, err := service.CreateCollection(1, []int64{firstID, otherUserID}, "", nil, nil); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("cross-user collection error = %v, want %v", err, ErrFileNotFound)
	}

	collection, err := service.CreateCollection(1, []int64{firstID, secondID}, "secret", nil, nil)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	public, err := service.GetPublicCollectionFromIP(collection.Token, "secret", HashIP("203.0.113.1"))
	if err != nil {
		t.Fatalf("get public collection: %v", err)
	}
	if len(public.Files) != 2 {
		t.Fatalf("public collection files = %#v, want two", public.Files)
	}
	if _, err := repo.db.Exec(`UPDATE user_files SET status = 'deleted' WHERE id = $1`, firstID); err != nil {
		t.Fatalf("soft delete collection item: %v", err)
	}
	if _, err := service.GetPublicCollectionFromIP(collection.Token, "secret", HashIP("203.0.113.1")); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("deleted source collection error = %v, want %v", err, ErrShareNotFound)
	}
}
