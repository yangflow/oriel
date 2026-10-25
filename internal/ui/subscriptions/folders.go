package subscriptions

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
	"github.com/yangflow/oriel/internal/ui/icons"
	"github.com/yangflow/oriel/internal/ui/theme"
)

// maxFolderName is the longest folder name in characters, as the server takes it.
const maxFolderName = 60

// foldering is the dialog that names a folder: a new one, or a new name for one that exists. A subscription to put in
// the new folder, from "Move to folder", comes along.
type foldering struct {
	open bool
	form ui.ScrollState
	id   int64         // the folder being renamed, 0 for a new one
	move client.Source // the subscription that goes into the new folder, ID 0 for none
	name string
	err  string
	busy bool
}

// folders are the folders the timeline has, by name.
func (s *Screen) folders() []client.Folder {
	if s.deps.Folders == nil {
		return nil
	}
	return s.deps.Folders()
}

// NewFolder opens the dialog that creates a folder.
func (s *Screen) NewFolder() {
	if s.api == nil {
		return
	}
	s.folderDlg = foldering{open: true}
}

// newFolderFor opens the dialog that creates a folder and moves src into it.
func (s *Screen) newFolderFor(src client.Source) {
	if s.api == nil {
		return
	}
	s.folderDlg = foldering{open: true, move: src}
}

// RenameFolder opens the dialog that renames f.
func (s *Screen) RenameFolder(f client.Folder) {
	if s.api == nil {
		return
	}
	s.folderDlg = foldering{open: true, id: f.ID, name: f.Name}
}

// AskDeleteFolder asks whether to delete f. Its subscriptions stay.
func (s *Screen) AskDeleteFolder(f client.Folder) {
	if s.api == nil {
		return
	}
	s.doomedFolder, s.deletingFolder = f, true
}

// FolderMenu fills the context menu of the folder f in the sidebar.
func (s *Screen) FolderMenu(m *ui.Menu, f client.Folder) {
	if m.Item(s.text.FolderRename).Chosen() {
		s.RenameFolder(f)
	}
	m.Separator()
	if m.Item(s.text.FolderDelete).Chosen() {
		s.AskDeleteFolder(f)
	}
}

// moveMenu is the "Move to folder" submenu of src: no folder, each folder with a check by the one it is in, and a new
// folder to put it in.
func (s *Screen) moveMenu(m *ui.Menu, src client.Source) {
	folders := s.folders()
	m.Submenu(s.text.MoveToFolder, func(sub *ui.Menu) {
		if sub.Item(s.text.NoFolder).Checked(src.FolderID == 0).Chosen() && src.FolderID != 0 {
			s.MoveToFolder(src, client.Folder{})
		}
		for _, f := range folders {
			if sub.Item(f.Name).Checked(src.FolderID == f.ID).Chosen() && src.FolderID != f.ID {
				s.MoveToFolder(src, f)
			}
		}
		sub.Separator()
		if sub.Item(s.text.NewFolderItem).Chosen() {
			s.newFolderFor(src)
		}
	})
}

// MoveToFolder puts src in the folder f, or takes it out of its folder when f has no ID.
func (s *Screen) MoveToFolder(src client.Source, f client.Folder) {
	if s.api == nil {
		return
	}
	gen, api := s.gen, s.api
	s.deps.Go(func() func() {
		changed, err := api.UpdateSource(context.Background(), src.ID, client.SourcePatch{FolderID: &f.ID})
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.deps.Changed(changed)
				s.toast = s.fill(s.text.FolderUnfiled, Name(changed))
				if f.ID != 0 {
					s.toast = strings.ReplaceAll(s.fill(s.text.FolderMoved, Name(changed)), "{folder}", f.Name)
				}
			case code(err) == "not_found":
				s.gone(src.ID)
			case code(err) == "invalid_folder":
				s.toast = s.text.FolderGone
				s.refiled()
			default:
				s.toast = s.text.ActionFailed
			}
		}
	})
}

// submitFolder creates the folder or renames it, and moves the subscription in when there is one.
func (s *Screen) submitFolder() {
	d := &s.folderDlg
	if d.busy || s.api == nil {
		return
	}
	name := strings.TrimSpace(d.name)
	if name == "" || utf8.RuneCountInString(name) > maxFolderName {
		d.err = s.text.FolderNameError
		s.announce = d.err
		return
	}
	d.busy, d.err = true, ""
	gen, api, id, move := s.gen, s.api, d.id, d.move
	s.deps.Go(func() func() {
		var f client.Folder
		var err error
		if id == 0 {
			f, err = api.CreateFolder(context.Background(), name)
		} else {
			f, err = api.RenameFolder(context.Background(), id, name)
		}
		var moved client.Source
		var moveErr error
		if err == nil && move.ID != 0 {
			moved, moveErr = api.UpdateSource(context.Background(), move.ID, client.SourcePatch{FolderID: &f.ID})
		}
		return func() {
			if gen != s.gen || s.expired(err) || s.expired(moveErr) {
				return
			}
			s.folderDlg.busy = false
			switch {
			case err == nil:
				s.folderDlg.open = false
				s.refiled()
				switch {
				case move.ID != 0 && moveErr == nil:
					s.deps.Changed(moved)
					s.toast = strings.ReplaceAll(s.fill(s.text.FolderMoved, Name(moved)), "{folder}", f.Name)
				case move.ID != 0:
					s.toast = s.text.ActionFailed
				case id != 0:
					s.toast = s.fill(s.text.FolderRenamed, f.Name)
				default:
					s.toast = s.fill(s.text.FolderCreated, f.Name)
				}
			case code(err) == "folder_exists":
				s.folderDlg.err = s.text.FolderExists
				s.announce = s.folderDlg.err
			case code(err) == "invalid_name":
				s.folderDlg.err = s.text.FolderNameError
				s.announce = s.folderDlg.err
			case code(err) == "not_found":
				s.folderDlg.open = false
				s.toast = s.text.FolderGone
				s.refiled()
			default:
				s.folderDlg.err = s.problem(err)
				s.announce = s.folderDlg.err
			}
		}
	})
}

// removeFolder deletes the folder that was asked about; its subscriptions are kept without a folder.
func (s *Screen) removeFolder() {
	if s.api == nil {
		return
	}
	gen, api, f := s.gen, s.api, s.doomedFolder
	s.deps.Go(func() func() {
		err := api.DeleteFolder(context.Background(), f.ID)
		return func() {
			if gen != s.gen || s.expired(err) {
				return
			}
			switch {
			case err == nil:
				s.toast = s.fill(s.text.FolderDeleted, f.Name)
				s.refiled()
			case code(err) == "not_found":
				s.toast = s.text.FolderGone
				s.refiled()
			default:
				s.toast = s.problem(err)
			}
		}
	})
}

// folderDialog is the dialog that names a folder.
func (s *Screen) folderDialog(c *ui.Context) {
	d := &s.folderDlg
	title := s.text.FolderNewTitle
	if d.id != 0 {
		title = s.text.FolderRenameTitle
	}
	s.dialog(c, &d.open, &d.form, 400, title, func() {
		s.headerBusy(c, &d.open, func() { tile(c, icons.FolderPlus) }, title, "", d.busy)
	}, func() {
		t := c.Theme()
		label(c, s.text.FolderName)
		box(c, "folder-name", nil, d.err != "", t.Surface, func() {
			in := ui.TextInputBase(c, &d.name).Grow(1).MinWidth(0).FocusRing(false).Label(s.text.FolderName).
				Placeholder(s.text.FolderNamePlaceholder).Disabled(d.busy)
			in.Focus()
			if in.Submitted() {
				s.submitFolder()
			}
		})
		if d.err != "" {
			errorLine(c, d.err)
		}
	}, func() {
		ui.Box(c).Grow(1)
		cancel := theme.Button{Style: theme.Secondary, Disabled: d.busy, Text: s.text.Cancel}.Draw(c)
		if cancel.Clicked() {
			d.open = false
		}
		text := s.text.FolderCreate
		switch {
		case d.busy:
			text = s.text.Saving
		case d.id != 0:
			text = s.text.Save
		}
		save := theme.Button{Style: theme.Primary, Disabled: d.busy, Text: text, Busy: d.busy}.Draw(c)
		if save.Clicked() {
			s.submitFolder()
		}
	})
}

// refiled tells the timeline that folders or the folders of subscriptions changed.
func (s *Screen) refiled() {
	if s.deps.Refiled != nil {
		s.deps.Refiled()
	}
}

// folderName is what the folder picker shows for the folder id.
func (s *Screen) folderName(id int64) string {
	for _, f := range s.folders() {
		if f.ID == id {
			return f.Name
		}
	}
	return s.text.NoFolder
}

// folderField is the folder picker of the edit dialog. It is not there while there are no folders and the
// subscription is in none.
func (s *Screen) folderField(c *ui.Context) {
	t := c.Theme()
	e := &s.edit
	folders := s.folders()
	if len(folders) == 0 {
		return
	}
	options := []int64{0}
	for _, f := range folders {
		options = append(options, f.ID)
	}
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		s.choiceSelect(c, &e.folder, options, s.text.Folder, s.folderName(e.folder), s.folderName, e.busy, e.folder == 0)
		help(c, s.text.FolderHelp)
	})
}
