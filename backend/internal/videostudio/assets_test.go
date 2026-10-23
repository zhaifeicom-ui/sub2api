package videostudio

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAssetReferenceTypeAndOwner(t *testing.T) {
	for _, tc := range []struct {
		name, mime, kind string
		owner            int64
		bad              bool
	}{
		{"image", "image/png", "image", 1, false},
		{"video", "video/mp4", "video", 1, false},
		{"audio", "audio/mpeg", "audio", 1, false},
		{"wrong type", "video/mp4", "image", 1, true},
		{"wrong owner", "image/png", "image", 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			mock.ExpectQuery("SELECT user_id,mime FROM video_studio_assets").WithArgs("asset_test").WillReturnRows(sqlmock.NewRows([]string{"user_id", "mime"}).AddRow(tc.owner, tc.mime))
			urls, err := (&Store{DB: db}).AssetURLsForType(context.Background(), 1, []string{"asset_test"}, "https://station.example", tc.kind)
			if (err != nil) != tc.bad {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.bad && (len(urls) != 1 || urls[0] != "https://station.example/api/v1/video-assets/asset_test") {
				t.Fatalf("bad URLs: %v", urls)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeneratedOutputStoredOnce(t *testing.T) {
	for _, exists := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT id FROM users").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectQuery("SELECT EXISTS").WithArgs("asset_result_test", int64(1)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(exists))
		if !exists {
			mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(0))
			mock.ExpectExec("INSERT INTO video_studio_assets").WithArgs("asset_result_test", int64(1), "result.mp4", "video/mp4", []byte("test")).WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectCommit()
		_, err = (&Store{DB: db}).putAsset(context.Background(), 1, "asset_result_test", "result.mp4", "video/mp4", []byte("test"))
		if err != nil {
			t.Fatal(err)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
	}
}
