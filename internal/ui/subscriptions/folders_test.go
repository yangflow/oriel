package subscriptions

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/yangflow/oriel/internal/client"
)

// withFolders gives the sample two folders and puts Go Blog in "Reading".
func withFolders(f *fake) {
	f.folders = []client.Folder{{ID: 7, Name: "Reading"}, {ID: 8, Name: "Tech"}}
	f.sources[0].FolderID = 7
}

// menuTester draws the context menu of each subscription, as the sidebar does, with the dialogs over it.
func menuTester(s *Screen, f *fake) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		for _, src := range f.sources {
			ui.Text(c, Name(src)).ContextMenu(func(m *ui.Menu) { s.Menu(m, src) })
		}
		for _, fo := range f.folders {
			ui.Text(c, "folder "+fo.Name).ContextMenu(func(m *ui.Menu) { s.FolderMenu(m, fo) })
		}
		s.View(c)
	}, 1000, 760)
	tt.Frame()
	return tt
}

// The edit dialog picks the folder of a subscription, saves only a changed choice and takes it out of the folder.
func TestEditFolderPicker(t *testing.T) {
	s, f, tt := setup(t, "en")
	withFolders(f)
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if !tt.HasText("Folder") || !tt.HasText("Reading") || !tt.HasText(s.text.FolderHelp) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.patches[1]) != 0 {
		t.Fatalf("saving without a change sent %+v", f.patches[1])
	}

	s.Edit(f.sources[0])
	tt.Frame()
	if err := tt.Click("Reading"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Tech"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	p := f.patches[1]
	if len(p) != 1 || p[0].FolderID == nil || *p[0].FolderID != 8 || p[0].Title != nil {
		t.Fatalf("patches %+v", p)
	}
	if len(f.changed) != 1 || f.changed[0].FolderID != 8 {
		t.Fatalf("changed %+v", f.changed)
	}

	s.Edit(f.sources[0])
	tt.Frame()
	if err := tt.Click("Tech"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("No folder"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 2 || p[1].FolderID == nil || *p[1].FolderID != 0 {
		t.Fatalf("patches %+v", p)
	}
}

// Without folders the dialog has no picker.
func TestEditFolderPickerHidden(t *testing.T) {
	s, f, tt := setup(t, "en")
	tt.SetSize(1000, 1600)
	s.Edit(f.sources[0])
	tt.Frame()
	f.settle(tt)
	if tt.HasText(s.text.FolderHelp) {
		t.Fatalf("a picker without folders: %v", tt.Texts())
	}
}

// The context menu moves a subscription to a folder, out of it, and into a folder made on the way.
func TestMoveToFolderMenu(t *testing.T) {
	s, f, _ := setup(t, "en")
	withFolders(f)
	tt := menuTester(s, f)
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "Move to folder") {
		t.Fatalf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Move to folder", "Tech"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 1 || p[0].FolderID == nil || *p[0].FolderID != 8 || len(f.changed) != 1 {
		t.Fatalf("patches %+v, changed %+v", p, f.changed)
	}
	tt.Frame()
	if !tt.HasText("Moved Go Blog to “Tech”") {
		t.Fatalf("texts %v", tt.Texts())
	}

	// Choosing the folder it is in does nothing; No folder takes it out.
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move to folder", "Tech"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.patches[1]) != 1 {
		t.Fatalf("patches %+v", f.patches[1])
	}
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move to folder", "No folder"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if p := f.patches[1]; len(p) != 2 || p[1].FolderID == nil || *p[1].FolderID != 0 {
		t.Fatalf("patches %+v", p)
	}

	// New folder… names a folder and puts the subscription in it.
	if err := tt.RightClick("Go Blog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Move to folder", "New folder…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("Later")
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.folderCalls, []string{"create Later"}) || f.refiled == 0 {
		t.Fatalf("folder calls %v, refiled %d", f.folderCalls, f.refiled)
	}
	if p := f.patches[1]; len(p) != 3 || p[2].FolderID == nil || *p[2].FolderID != 102 || s.folderDlg.open {
		t.Fatalf("patches %+v, dialog %+v", p, s.folderDlg)
	}
}

// A new folder needs a name of 1 to 60 characters and one nobody uses yet.
func TestNewFolderDialog(t *testing.T) {
	s, f, tt := setup(t, "en")
	withFolders(f)
	s.NewFolder()
	tt.Frame()
	if !tt.HasText("New folder") {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.folderCalls) != 0 || !tt.HasText(s.text.FolderNameError) {
		t.Fatalf("calls %v, texts %v", f.folderCalls, tt.Texts())
	}
	tt.Type("Reading")
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !s.folderDlg.open || !tt.HasText(s.text.FolderExists) {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type("2")
	if err := tt.Click("Create"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if s.folderDlg.open || f.refiled != 1 || f.folderCalls[len(f.folderCalls)-1] != "create Reading2" {
		t.Fatalf("calls %v, refiled %d", f.folderCalls, f.refiled)
	}
	tt.Frame()
	if !tt.HasText("Created the folder “Reading2”") {
		t.Fatalf("texts %v", tt.Texts())
	}
}

// A folder is renamed and deleted from its context menu; deleting asks first and says its subscriptions stay.
func TestRenameAndDeleteFolder(t *testing.T) {
	s, f, _ := setup(t, "en")
	withFolders(f)
	tt := menuTester(s, f)
	if err := tt.RightClick("folder Reading"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Rename…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Rename folder") {
		t.Fatalf("texts %v", tt.Texts())
	}
	tt.Type(" Later")
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if !slices.Equal(f.folderCalls, []string{"rename 7 Reading Later"}) || f.refiled != 1 {
		t.Fatalf("calls %v, refiled %d", f.folderCalls, f.refiled)
	}

	if err := tt.RightClick("folder Tech"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete folder…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Delete the folder “Tech”?") || !tt.HasText(s.text.FolderDeleteBody) {
		t.Fatalf("texts %v", tt.Texts())
	}
	if err := tt.Click(s.text.Cancel); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if len(f.folders) != 2 {
		t.Fatalf("cancel deleted: %v", f.folderCalls)
	}
	if err := tt.RightClick("folder Tech"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Delete folder…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(s.text.ConfirmDelete); err != nil {
		t.Fatal(err)
	}
	f.settle(tt)
	if f.folderCalls[len(f.folderCalls)-1] != "delete 8" || f.refiled != 2 {
		t.Fatalf("calls %v, refiled %d", f.folderCalls, f.refiled)
	}
}

// The dialogs speak Chinese.
func TestFolderDialogInChinese(t *testing.T) {
	s, f, tt := setup(t, "zh-Hans")
	withFolders(f)
	s.NewFolder()
	tt.Frame()
	if !tt.HasText("新建文件夹") || !tt.HasText("创建") {
		t.Fatalf("texts %v", tt.Texts())
	}
	s.folderDlg.open = false
	s.AskDeleteFolder(f.folders[0])
	tt.Frame()
	if !tt.HasText("删除文件夹「Reading」？") || !tt.HasText("里面的订阅会保留，变成没有文件夹。") {
		t.Fatalf("texts %v", tt.Texts())
	}
}
