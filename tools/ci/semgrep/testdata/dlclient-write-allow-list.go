//go:build semgrep_fixture

package fixture

import (
	"context"

	"github.com/golusoris/goenvoy/downloadclient/deluge/v2"
	"github.com/golusoris/goenvoy/downloadclient/nzbget/v2"
	"github.com/golusoris/goenvoy/downloadclient/qbit/v2"
	q "github.com/golusoris/goenvoy/downloadclient/qbit/v2"
	"github.com/golusoris/goenvoy/downloadclient/rtorrent/v2"
	"github.com/golusoris/goenvoy/downloadclient/sabnzbd/v2"
	"github.com/golusoris/goenvoy/downloadclient/transmission/v2"
)

func qbitTest(ctx context.Context, cl *qbit.Client, aliasCl *q.Client) {
	// ok: dlclient-write-allow-list
	_ = cl.Login(ctx, "user", "pass")
	// ok: dlclient-write-allow-list
	_, _ = cl.Version(ctx)
	// ok: dlclient-write-allow-list
	_, _ = cl.ListTorrents(ctx, nil)
	// ok: dlclient-write-allow-list
	_, _ = cl.GetPreferences(ctx)
	// ok: dlclient-write-allow-list
	_ = cl.TopPriority(ctx, []string{"hash1"})
	// ok: dlclient-write-allow-list
	_ = cl.BottomPriority(ctx, []string{"hash1"})
	// ok: dlclient-write-allow-list
	_ = cl.IncreasePriority(ctx, []string{"hash1"})
	// ok: dlclient-write-allow-list
	_ = cl.DecreasePriority(ctx, []string{"hash1"})
	// ok: dlclient-write-allow-list
	_ = cl.StopTorrents(ctx, []string{"hash1"})
	// ok: dlclient-write-allow-list
	_ = cl.StartTorrents(ctx, []string{"hash1"})

	// ruleid: dlclient-write-allow-list
	_ = cl.PauseTorrents(ctx, []string{"hash1"})
	// ruleid: dlclient-write-allow-list
	_ = cl.ResumeTorrents(ctx, []string{"hash1"})
	// ruleid: dlclient-write-allow-list
	_ = aliasCl.DeleteTorrents(ctx, []string{"hash1"}, true)
}

func sabTest(ctx context.Context, cl *sabnzbd.Client) {
	// ok: dlclient-write-allow-list
	_, _ = cl.GetVersion(ctx)
	// ok: dlclient-write-allow-list
	_, _ = cl.GetQueue(ctx)
	// ok: dlclient-write-allow-list
	_, _ = cl.SetPriority(ctx, "nzo1", sabnzbd.PriorityNormal)
	// ok: dlclient-write-allow-list
	_ = cl.PauseItem(ctx, "nzo1")
	// ok: dlclient-write-allow-list
	_ = cl.ResumeItem(ctx, "nzo1")

	// ruleid: dlclient-write-allow-list
	_ = cl.Pause(ctx)
	// ruleid: dlclient-write-allow-list
	_ = cl.Resume(ctx)
}

func nzbgetTest(ctx context.Context, cl *nzbget.Client) {
	// ok: dlclient-write-allow-list
	_, _ = cl.GetVersion(ctx)
	// ok: dlclient-write-allow-list
	_, _ = cl.GetStatus(ctx)
	// ok: dlclient-write-allow-list
	_, _ = cl.ListGroups(ctx)
	// ok: dlclient-write-allow-list
	_, _ = cl.SetPriority(ctx, 1, 100)
	// ok: dlclient-write-allow-list
	_, _ = cl.EditQueue(ctx, "GroupPause", "", []int{1})
	// ok: dlclient-write-allow-list
	_, _ = cl.EditQueue(ctx, "GroupResume", "", []int{1})

	// ruleid: dlclient-write-allow-list
	_ = cl.PauseDownload(ctx)
	// ruleid: dlclient-write-allow-list
	_ = cl.ResumeDownload(ctx)
	// ruleid: dlclient-write-allow-list
	_, _ = cl.EditQueue(ctx, "GroupDelete", "", []int{1})
	// ruleid: dlclient-write-allow-list
	_, _ = cl.EditQueue(ctx, "GroupDelete", "GroupPause", []int{1})
}

func observeOnlyTest(ctx context.Context, tr *transmission.Client, del *deluge.Client, rt *rtorrent.Client) {
	// ok: dlclient-write-allow-list
	_, _ = tr.GetSession(ctx)
	// ok: dlclient-write-allow-list
	_, _ = tr.GetTorrents(ctx, nil)

	// ok: dlclient-write-allow-list
	_ = del.Login(ctx, "secret")
	// ok: dlclient-write-allow-list
	_, _ = del.Connected(ctx)
	// ok: dlclient-write-allow-list
	_, _ = del.GetTorrentsStatus(ctx, nil)
	// ok: dlclient-write-allow-list
	_, _ = del.GetVersion(ctx)

	// ok: dlclient-write-allow-list
	_, _ = rt.GetSystemInfo(ctx)
	// ok: dlclient-write-allow-list
	_, _ = rt.GetTorrents(ctx, "main")
}
