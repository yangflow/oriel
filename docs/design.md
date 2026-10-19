# Oriel design language

This is the design language of Oriel: the rules every screen follows, and the place to record where a screen
departs from them. Oriel is a single-user reader that mixes RSS and RSSHub subscriptions into one timeline of
articles, videos, social posts and images (see [product.md](product.md), in Chinese). The same program is the
desktop app and, with `oriel serve`, a headless server; only the desktop app has a UI, and that UI reads and writes
through the HTTP API.

The UI is built on the native toolkit of [MyGo](https://github.com/egoist/mygo). This document summarizes the parts
of its [UI guides](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/README.md) that matter for design; the guides
stay the authority on API details. Links point to MyGo v0.3.6, the version pinned in `go.mod`. When the pin moves,
check the pages again and update the links and this document together.

Five screens are built: the connect screen in `internal/ui/connect/` (mockup `design/screens/connect.html`), which
connects the desktop app to a remote server, the timeline in `internal/ui/timeline/` (mockup
`design/screens/timeline.html`), which is home, the subscription dialogs over it in `internal/ui/subscriptions/`
(mockup `design/screens/subscriptions.html`), the reader in `internal/ui/reader/` (mockup
`design/screens/reader.html`), which takes the list's place for an article, and the settings in
`internal/ui/settings/` (mockup `design/screens/settings.html`), which take the whole window.

Related files: [design/README.md](../design/README.md) (mockups and how a UI change is made) and
[design/tokens.md](../design/tokens.md) (spacing, size and color values in one table).

## Contents

1. [Principles](#principles)
2. [Layout and spacing](#layout-and-spacing)
3. [Color and dark mode](#color-and-dark-mode)
4. [Typography](#typography)
5. [Icons and images](#icons-and-images)
6. [Component guide](#component-guide)
7. [Windows, menus and navigation](#windows-menus-and-navigation)
8. [Accessibility checklist](#accessibility-checklist)
9. [Motion](#motion)
10. [Localization](#localization)
11. [Design acceptance checklist](#design-acceptance-checklist)
12. [References](#references)

## Principles

- **Native first.** Use MyGo's native widgets and the theme defaults as they are, and follow the system's light and
  dark appearance and accent color. A control that looks and behaves like the platform's is learned already. Style
  an element yourself only when no widget does the job, and build it on the
  [custom widget](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/custom-widgets.md) bases so focus, keyboard and
  accessibility stay intact. No WebView and no HTML frontend.
- **Consistent across platforms, respectful of each.** One view serves macOS, Windows and Linux. Follow each
  platform's conventions where MyGo exposes them: menu roles, the accent color, the system font, shortcuts that use
  `CmdOrCtrl`. Do not add chrome that imitates one platform on the others.
- **Content first.** Oriel is for reading. The timeline and the reader give the room to titles, text and images;
  controls stay quiet and secondary until they are needed.
- **Accessible by default.** Every element can be reached with the keyboard, named for a screen reader, read in both
  appearances, and still works at larger text sizes. See the [checklist](#accessibility-checklist).
- **No custom motion.** State changes take effect at once. See [Motion](#motion).
- **Meaning is never carried by color alone.** Say it in words; pair color with text, a symbol or a shape.
- **The UI thread draws.** A frame is built from in-memory state. No disk, network or other blocking work in a view,
  including calls to Oriel's own HTTP API and decoding images; start that work in the background and change the state
  back on the UI thread (see [Views](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/views.md)).
- **Spec before code.** Every screen is specified in its mockup in `design/` and in this document before it is
  written. Only `design/` and this document count as the design reference for Oriel.

## Layout and spacing

Layout uses flexbox in device-independent pixels (DIPs); see
[Layout](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/layout.md).

- **One spacing unit.** The theme's `Spacing` (4 DIPs by default; 3 is compact, 5 roomy) is the unit of every widget's
  padding and gap. Size your own elements with `t.Space(n)` so they follow it. Do not write raw pixel numbers for
  spacing.
- **Scale.** Use whole multiples of the unit: 1 (4) between a label and its control, 2 (8) between related controls
  and between buttons, 4 (16) between groups, 6 (24) for the window's outer padding. Larger gaps need a reason, for
  example a section break.
- **Structure from containers.** `Column` and `Row` with `Gap` and `Padding`; `Grow(1)` and `Spacer` for free space;
  `Grid` for forms and aligned tables of controls. Prefer `Gap` to margins on individual children.
- **Sizes are bounds.** Give windows a minimum size and let content reflow; do not fix a width a translation can
  overflow. Use `MinWidth`, `MaxWidth` and `Grow` instead of fixed widths, except for square symbol buttons.
- **Alignment.** Left-align text and labels; center only isolated focal content, as an empty state. Reading
  text in the article reader gets a comfortable maximum line length (`MaxWidth`) instead of spanning a wide window.
- **Scrolling.** Content that can exceed the window goes in a
  [scroll view](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/scroll.md). The timeline is a
  [list](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/list.md), which builds only the rows in view and lets rows
  of different kinds have different heights.
- **The connect screen.** A sheet at most `Space(80)` (320) wide, centered in a scroll view with outer padding
  `Space(6)`; `Space(6)` between the header, the form and the buttons, `Space(2)` between fields, `Space(1.5)` (6)
  between a field and its error. Fields and buttons are `Space(8)` (32) high; the two buttons share the width
  equally.
- **The timeline.** A sidebar, a 1 DIP divider and the main column. The sidebar is 212 wide (184 below a 760 wide
  window) on `Surface`: five kind tabs on top, 44 high, each an icon over its unread count; then the subscriptions
  of the chosen kind, 28 high rows; at the bottom the server bar, 40 high. The main column has a 48 high toolbar
  (title, count, the Everything/Unread/Starred segments, search, mark all as read), a divider, and the entries.
  Article, social and mixed rows sit in one column at most 680 wide (720 for Social), centered; images in a social
  post share a grid at most 440 wide (520 in Social). Videos are a grid of cards at least 200 wide, 16:9 covers;
  Images are a masonry wall of up to three columns at least 170 wide, each picture at its own aspect ratio.
  Below a 760 wide window the segments show icons only. Sizes taken from the mockup are written `px(t, n)`, which is
  `t.Rem(n / 13)`, so they follow the text size. The sidebar's section header, 22 high, ends in a 22 square + button
  that adds a subscription; it shows even when there are none. A paused subscription has its name in `TextMuted`, a
  pause mark instead of the failure mark, and no count. With one subscription chosen, a pencil button after the
  count edits it.
- **The subscription dialogs.** Dialogs over the timeline on a `RGBA(0,0,0,.28)` backdrop: 440 wide to add, 480 to
  edit, radius 12, `Border` and a soft shadow, at most the window's height less `Space(8)`. A header (a 32 square
  tile or monogram, the title 15 semibold, a 28 square close button), a form that scrolls when the window is too low,
  with `Space(3.5)` between fields and 20 to the sides, and a footer with the buttons at the right; in the edit
  dialog "Delete…" sits alone at the left, in `Danger` without a fill. Fields are 32 high; title and interval share a
  row. Under the address, one line at a time: the error, the help, the RSSHub instance card (`Warning` tint) or what
  the address was recognized as (`Accent` tint, the address in monospace). The edit dialog starts with a health line
  (`Surface`, or the `Warning` tint while fetches fail) with a small Refresh button. It ends with "Summaries and
  translations", a 12 semibold muted group title with the sparkles icon: two switches that queue new entries, and two
  monospace prompt boxes, three lines that grow to eight, whose placeholder says the empty box uses the prompt in
  Settings. While AI is not set up a line on `Surface` in `TextMuted` says so, with a small "Open Settings…".
- **The reader.** It takes the main column's place; the sidebar stays. A 48 high toolbar: "Back to {scope}" (a chevron
  and the scope's name) at the left; previous, next, a separator, Summarize (sparkles) and Translate (languages), a
  separator, star (`Warning` when starred), read toggle and open in browser at the right, 28 square buttons,
  separators 1 DIP. Summarize and Translate are toggles: pressed, they sit on `AccentSoft` in `Accent` and their names
  become "Hide the summary" and "Show the original". Under the divider the article scrolls, a column at most 680 wide,
  centered, padded 28/32/48 (20 below a 760 wide window): the source's monogram, name and full date at 12; the title
  at 24 (20 when narrow) weight 650; the note on where the text comes from (`Warning` tint when it is the feed's,
  `Surface` with a spinner while the full text is being got), 12.5, its "Open the original" link underlined; then the
  text 22 below. Body text is 15 at line height 1.65; h2 18 and h3 15.5 semibold, 26 and 22 above; 14 between
  paragraphs, 4 between list items, a 22 wide marker column; quotes behind a 3 DIP `Border` line in `TextMuted`; code
  on `Surface` at 13 monospace, scrolling sideways; links in `Accent`, underlined; pictures at their own aspect ratio
  (16:9 until known), at most their own width, captions 12 in `TextMuted`. Loading is a skeleton; the empty states
  (getting the text, no text, can't load) are a 48 tile, a 15 semibold heading, a hint at most 320 wide and a button.
  The whole text is one selection.
- **Summary and translation in the reader.** The summary is a card under the title, radius 10, padded 12/14, text 14
  at line height 1.6, its head 12 semibold: while it is made, `AccentSoft` with a spinner, "Summarizing…", a Cancel
  text button and three skeleton lines; done, `AccentSoft` with "Summary · by {model}", the model in `TextMuted`, and
  a close button; failed, the `Danger` tint with what went wrong and a small "Try again"; while AI is not set up,
  `Surface` with "AI isn't set up", one line on where to set it and a small primary "Open Settings…". The translation
  replaces the title and text in place; the note under the title says what is shown. While it is made, the original
  stays with a note (`Surface`, a spinner, Cancel); done, the note is `AccentSoft`, "Translated by {model}" and a
  segmented Translation | Original switch, and the translated text is plain paragraphs, pictures and links staying in
  the original; failed, the original stays with the note in the `Danger` tint, the reason and "Try again". Cancel only
  stops waiting; leaving the article does the same.

## Color and dark mode

Colors come from the theme, `c.Theme()`. Oriel puts its own palette, warm neutrals and a teal accent, on the theme
roles: the root view calls `theme.Apply` (in `internal/ui/theme/`) first in every frame, which picks the light or
dark palette for the system appearance and keeps the theme's metrics, so the desktop's text size still applies. The
values are in [design/tokens.md](../design/tokens.md). See
[Styling and themes](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/styling.md).

- **Always use theme roles, never literals.** Screens use only theme colors; for example, secondary text uses
  `TextMuted`. Color literals live only in `internal/ui/theme/`. A faint fill behind an icon or a status is
  `theme.Soft` of a role.

  | Role | Use |
  |---|---|
  | `Background` | window and page fill |
  | `Surface`, `SurfaceHover`, `SurfacePressed` | faces of controls and cards, and their states |
  | `Border` | outlines and dividers |
  | `Text`, `TextMuted` | primary text; secondary text, hints and placeholders |
  | `Accent`, `AccentHover`, `AccentPressed`, `AccentText` | primary action, selection and checked controls; text on the accent |
  | `Danger`, `Warning`, `Success` | destructive, cautionary and positive status (always with text or a symbol) |
  | `Selection`, `Focus` | selected text; the keyboard focus ring |
  | `Inverse`, `InverseText` | tooltips and toasts (left zero, they are `Text` and `Background`) |
  | `Scrollbar` | scroll bar thumbs |

- **Appearance.** Do not implement your own dark mode switch; the theme follows the system. If a screen draws custom
  elements, use only theme colors, or read `c.Theme().Dark` where a choice cannot be expressed with them. For derived
  colors use `Mix` and `Alpha` on theme colors rather than new constants.
- **Accent.** Oriel's accent is teal, in both appearances; it does not follow the system accent. One primary action
  per group gets the accent.
- **Contrast.** Text and meaningful graphics should meet WCAG 2.1 AA: 4.5:1 for body text, 3:1 for large text and
  control boundaries. With the desktop's high contrast setting on (`c.Preferences().HighContrast`), `theme.Apply`
  moves `Border` and `TextMuted` toward `Text` and makes `Focus` opaque; test with it on.
- **Status.** Color supplements a message; the message states the status in words. A read entry, a failing
  subscription or a missing full text is said in text, not only shown as a dimmed or red row.
- **Content colors.** Images and video thumbnails from feeds keep their own colors; everything Oriel draws around
  them uses theme roles.
- **Mockups.** Mockups use the Oriel palette of `design/tokens.md`; the app running on a real device is what
  counts.

## Typography

- **System font.** Leave `Font` empty so text uses the platform's UI font. The default text size is 13 on macOS and
  14 elsewhere, and scales with the desktop's text size (`Preferences().TextScale`) when you rely on the theme. See
  [Text](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/text.md).
- **Size relative to the theme.** Use `t.Rem(n)` for sizes (`n` times the theme font size) rather than fixed numbers,
  so custom text follows the user's text scale. Widgets use the theme size.
- **The connect screen.** Title 1.8 rem bold, hint 0.9 rem in `TextMuted`.
- **The timeline.** Screen title 15 bold, entry titles 13.5 semibold (read entries in `TextMuted`), source and time
  12 in `TextMuted`, counts 10 to 11. Titles in rows and video cards wrap; summaries show at most two lines and
  social text six; a picture's caption in the wall is one line. The full title is always the entry's accessible
  name.
- **Hierarchy with few steps.** Body, secondary (`TextMuted`, about 0.9 rem), section heading (bold) and at most one
  display size per screen. Weight and color before size.
- **Headings are real headings.** Give the title of a screen or section `Role(ui.RoleHeading)` and a `Level` so
  assistive technology can navigate by them.
- **Do not truncate silently.** Prefer wrapping. Where a single line is needed (`SingleLine`, `MaxLines`), for
  example an entry title in the timeline, make the full text available in a tooltip or in the reader.
- **Numbers and dates.** Show counts and dates in the user's language when they are data, not identifiers. Digits
  that change in place can use `FontFeatures("tnum")`.

## Icons and images

- **Icons are SVG** shown with `ui.Icon`, which takes the text color and the font size: an icon follows theme colors
  and the text size automatically. See [Icon](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/icon.md).
- **One set: [Lucide](https://lucide.dev)** (ISC License), recorded in `THIRD_PARTY.md`. The files of lucide-static
  are copied unchanged into `internal/ui/icons/lucide/` and parsed once in `internal/ui/icons`; add an icon there
  before using it. Prefer an icon to a word where the icon is clearer, and keep the word where it is not.
- **An icon needs a name when it is alone.** Icons are decoration for assistive technology unless given a `Label`.
  A button that shows only an icon or a symbol must get a readable name with `.Label()`; an icon next to a visible
  text label needs none.
- **Icon before label** in buttons, except a forward arrow, which follows its label ("Connect →"). Icons in fields
  and buttons are 1.25 rem, in error lines 1.05 rem, sized with `FontSize`.
- **Feed images** (thumbnails, image entries, site icons) use
  [Image](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/image.md). Download and decode them in the background and
  make each bitmap once, never in the view; until it is ready, keep the image's box at its final size so the timeline
  does not jump. Images that carry meaning get a `Label`; decorative ones do not.
- **The timeline.** Each kind has an icon (newspaper, play, message circle, image; inbox for All) and a color:
  `Accent` for articles, `Danger` for videos, `Warning` for social, `Success` for images, used as a soft fill behind a
  subscription's monogram and on the chosen tab. Row actions are icons only (star, mark read, open in browser) with
  labels and tooltips, shown on hover and on the focused row. Pictures are downloaded through the server
  (`/api/image`), at most four at a time, and the 300 most recent bitmaps are kept; a missing picture keeps its box
  with an image icon. A video cover carries a play mark and the duration, labelled "Duration 12:34".
- **The product icon** is a mixed timeline on a warm ivory tile: a teal rail joins three entries, each a teal dot
  with a title line in ink (`#26231f`) and a shorter summary line in muted gray; the newest entry's dot, at the top,
  is the brightest (`#5eead4` to `#0f766e`). It sits on the macOS grid (an 824 tile at 100 in 1024, radius 185) with
  transparent corners, so every platform shows the same rounded tile. It was drawn for Oriel and is MIT licensed with
  the rest of the project. The source is [design/icon.svg](../design/icon.svg); see
  [design/README.md](../design/README.md) for exporting it.

## Component guide

Pick the widget by the job. Each name links to the upstream page, which also says what assistive technology sees of
the widget. The complete list is the
[UI index](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/README.md).

What Oriel uses today:

| Component | Use |
|---|---|
| `ui.Button` | Secondary actions. A button that shows only a symbol must give a readable name with `.Label()`. |
| `ui.PrimaryButton` | The main action of a group of buttons; at most one. |
| `ui.Text`, `ui.RichText` | Text; `RichText` where one word of a sentence is set apart. Secondary explanations use `TextMuted`. |
| `ui.TextInputBase` | A text field drawn by the screen, inside a box with its icon; `Error` names what is wrong for assistive technology, and the screen shows it under the field. The timeline's search is one too, since `SearchField` moves the focus to its box instead of the text. |
| `ui.ButtonBase` | Buttons drawn by the screen in the Oriel look: an icon and a label, or an icon alone with a `Label`. |
| `ui.Icon` | Lucide icons from `internal/ui/icons`. |
| `ui.SegmentedBase` | The timeline's kind tabs and its Everything/Unread/Starred filter, and the kind of a subscription, drawn in the Oriel look. |
| `ui.DialogBase` | The add and edit subscription dialogs: Escape and a click outside close them, the window's keys stop behind them, and the focus goes back where it was. |
| `ui.SelectBase`, `ui.Switch`, `ui.TextArea` | A subscription's fetch interval, its pause switch and its mute keywords. |
| `Element.ContextMenu` | Right click on a subscription: Refresh, Edit…, Pause or Resume, Delete…. |
| `ui.List`, `ui.GridView` | The timeline's rows and its video cards; both build only what is in view. |
| `ui.AlertDialog` | Confirming "Mark all as read" and deleting a subscription. Its last button, the action, is the default and takes the focus; Escape is Cancel. |
| `c.Toast`, `c.Announce` | Feedback after starring, marking and failures; counts announced without moving the focus. |

The tables below say what to reach for as screens are added.

### Actions

| Need | Use |
|---|---|
| The main action of a group, once | [Button](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/button.md) (`PrimaryButton`) |
| Other actions | [Button](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/button.md) |
| Open an entry's original page in the system browser | [Link](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/link.md) |
| Several related actions in one place | [Menu button](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/menu-button.md) |
| Actions on a specific entry or subscription, on right click | [Context menu](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/context-menu.md) |
| An on/off command that stays pressed | [Toggle](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/toggle.md) |
| Choose one of a few views or modes | [Segmented control](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/segmented.md) |
| Window-level commands | [Toolbar](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/toolbar.md) |

### Choices

| Need | Use |
|---|---|
| A setting applied immediately (for example a per-subscription switch) | [Switch](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/switch.md) |
| A yes/no value submitted with a form, or several independent options | [Checkbox](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/checkbox.md) |
| One of two to five visible options (for example a subscription's kind) | [Radio](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/radio.md) |
| One of many options | [Select](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/select.md); with typing to filter, [Combobox](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/combobox.md) |
| A value in a range | [Slider](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/slider.md); an exact number, [Number input](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/number-input.md) or [Stepper](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/stepper.md) |

### Text

| Need | Use |
|---|---|
| Free text, one or many lines (a feed URL, an RSSHub path, a prompt) | [Text input](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/text-input.md) |
| Searching the timeline | [Search field](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/search-field.md), or [Find bar](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/find-bar.md) for text in the reader |
| Mute keywords | [Token field](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/token-field.md) |
| Rename a subscription in place | [Editable text](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/editable-text.md) |
| Labeled controls with help and errors | [Form](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/form.md) |

### Collections

| Need | Use |
|---|---|
| The timeline, and other long lists of one kind | [List](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/list.md) |
| Rows with sortable, resizable columns (for example subscription health) | [Table](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/table.md) |
| Hierarchy | [Tree](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/tree.md), or [Outline](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/outline.md) when large |
| Thumbnails | [Grid view](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/grid-view.md) |

### Containers and navigation

| Need | Use |
|---|---|
| Scrolling content, such as the reader | [Scroll view](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/scroll.md) |
| Columns and rows of controls | [Grid](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/grid.md) |
| Two resizable panes (timeline and reader) | [Split view](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/split.md) |
| Optional detail | [Collapsible](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/collapsible.md), [Accordion](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/accordion.md) |
| Top-level sections of the app | [Sidebar](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/sidebar.md) |
| A few peer pages inside a screen | [Tabs](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/tabs.md) |
| Pages with a history | [Router](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/navigation.md), with [Back and forward buttons](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/history-buttons.md) |

### Dialogs, messages and status

| Need | Use |
|---|---|
| A task that needs the user's full attention | [Dialog](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/dialog.md) |
| Confirm something important or destructive (deleting a subscription and its entries) | [Alert dialog](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/alert-dialog.md); for system file and message dialogs (OPML import and export, the startup error), `mygo.Dialog` ([desktop APIs](https://github.com/egoist/mygo/blob/v0.3.6/docs/native.md)) |
| A small panel tied to an element | [Popover](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/popover.md) |
| A short label or hint for a control | [Tooltip](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/tooltip.md) (never the only place for essential information) |
| Feedback that needs no decision | [Toast](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/toast.md) |
| Known progress / unknown duration (a fetch, an AI summary) | [Progress bar](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/progress.md) / [Spinner](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/spinner.md) |
| A count or short status (unread entries) | [Badge](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/badge.md) |

Overlays share focus and Escape behavior; see
[Overlays](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/overlays.md). Rules of thumb:

- Prefer undo over confirmation. Confirm only what cannot be undone, with a button named for the action ("Delete"),
  not "OK", and Cancel as the Escape action.
- Do not stack dialogs. Use a toast for news that needs no answer.
- Errors say what happened and what to do, next to the control that caused them (`Error`, or a
  [form](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/form.md) field). The server returns stable error codes;
  the UI maps each to localized copy and never shows the raw code alone.
- AI summaries and translations are on demand and can be slow or fail: show that the work is going on, keep the
  original text readable meanwhile, and say plainly when it failed.

## Windows, menus and navigation

- **Windows.** One main window; set `Title`, `Width`, `Height`, and `MinWidth`/`MinHeight` so content never clips.
  Oriel's window is 960×640 with a minimum of 640×480. `StateKey` remembers size and position between runs and is not used yet. A second launch brings the existing
  window to the front. See
  [Windows with native UI](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/windows.md) and the
  [window options](https://github.com/egoist/mygo/blob/v0.3.6/docs/windows.md).
- **Title bar.** Keep the system title bar. A hidden title bar (`TitleBarStyle: mygo.TitleBarHidden`) makes the view
  responsible for the room the window controls take (`c.TitleBar()`) and for a draggable area (`DragWindow`); use it
  only with a mockup that shows why.
- **Vibrancy and materials.** macOS vibrancy shows where the view draws no background; if used at all, use it for a
  sidebar only, over opaque content. Oriel uses opaque theme surfaces and no custom glass.
- **Menus.** The macOS menu bar belongs to the app. Oriel's menu has an app menu (About, check for updates, Quit),
  the Edit menu (`RoleEditMenu`, so copy and paste work) and the Window menu (`RoleWindowMenu`); "Connect to
  Server…" in the app menu opens the connect screen and "Settings…" (Cmd/Ctrl+,) the settings. Use roles
  (`RoleAbout`, `RoleQuit` and the others) for standard items: they carry the right label and shortcut, and items
  that only macOS has are left out elsewhere. Give custom items an accelerator with `CmdOrCtrl`. Every command
  reachable by a toolbar button should also be in a menu. See
  [Menus](https://github.com/egoist/mygo/blob/v0.3.6/docs/menus.md).
- **Navigation.** Choose the lightest structure that fits: a single screen;
  [tabs](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/tabs.md) for a few peer views; a
  [sidebar](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/sidebar.md) with a
  [router](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/navigation.md) for several top-level sections (timeline,
  subscriptions, settings). A router gives back/forward keys, state restored per page and focus handling. Give every
  page a `Title`.
- **Leaving the app.** Video and social entries have no embedded player; opening them hands the link to the system
  browser. Say so in the control ("Open in browser"), not only with an icon.
- **Toolbar.** Frequent window-level commands, in order of importance; controls that do not fit overflow into a menu.
- **Notifications.** Use system notifications only for events the user would want when the app is in the background.
- **Timeline keys.** J or Down and K or Up move between entries, O or Enter opens the chosen one (an article in the
  reader, anything else in the browser) and marks it read, S stars it, M toggles read, `/` and Cmd/Ctrl+F go to search, Escape clears it. The list keys are
  window-wide but stop while the search field has the focus or a dialog is open, so typing never moves the list.
  Cmd/Ctrl+N adds a subscription.
  Every entry is also a focusable element with its own name, so Tab reaches it.
- **Reader keys.** While the reader shows, J and K go to the next and previous article, S stars, M toggles read, O
  opens the original in the browser and Escape goes back to the list, the entry it opened from focused. The article
  takes the focus when it opens, so the arrows, Space, Page Up/Down and Home/End scroll it, and Tab goes through its
  links. Every toolbar button names its key in the tooltip.
- **Settings.** The settings take the whole window, in place of the timeline: their own sidebar has "Back to the
  timeline" on top, one item per group (Fetching, Import and export, AI, Devices) and the server bar, which says whose
  settings these are and does not open the connect screen. They open from the gear beside the timeline's server bar
  or "Settings…" in the app menu; the back button and Escape return to the timeline as it was. A setting is saved
  where it is changed: the RSSHub instance with its Save button or Enter, the retention as soon as another is
  chosen; the line under it says it was saved, or why not, and a refused value keeps the focus in its field. OPML
  files are chosen and saved with the system's file dialogs, read and sent in the background; the result stays
  under the button, with the subscriptions that could not be added and why, until dismissed. Revoking a device asks
  first; the current device is marked and cannot be revoked. The AI group has the endpoint, model, API key and
  output language in one card with its own Save, and the two prompts in a second card. The key field is always
  empty; a badge says "Configured" or "Not configured", a new key replaces the old one and "Remove key" deletes it.
  Test sends a short request with the saved settings, so it is disabled while there are unsaved edits; its result
  stays beside the buttons. Each prompt has "Restore default", disabled while it is the default. Language and
  theme follow the system and are not settings.

## Accessibility checklist

MyGo exposes native UI to VoiceOver, Narrator, NVDA and Orca; see
[Accessibility](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/accessibility.md).

- [ ] Every interactive element has an accessible name: its text, or `Label` when it shows only an icon, a symbol or
      a drawing.
- [ ] `Description` carries help text; `Error` marks invalid values.
- [ ] Custom elements that act as widgets set `Role`, and the states they show (`Checked`, `Expanded`, `Range`).
- [ ] Section and screen titles are headings with a `Level`.
- [ ] Changes that do not move the focus (a count, new entries arriving, a finished summary) call `c.Announce(text)`.
      Toasts announce their text, and a router the title of a page shown while the focus stays outside it.
- [ ] The whole UI works with the keyboard: the focus order is the standard one and follows reading order, there is
      a visible focus ring (do not remove it), shortcuts exist for frequent actions, Escape closes overlays.
- [ ] Text uses theme sizes (`Rem`) and scales; layouts reflow at larger text and in the longer translation.
- [ ] Colors are theme roles; contrast holds in both appearances and with high contrast on.
- [ ] Nothing depends on color, sound or motion alone.
- [ ] Pointer targets are at least about 24×24 DIPs, and 32 or more for primary controls.
- [ ] `ui.NewTester` tests click controls by their accessible names, so a missing name fails the test (see
      [Testing](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/testing.md)).

Contrast, scaling, keyboard use and screen readers are verified on real devices; a passing headless test does not
count as accessibility acceptance.
The [Inspector](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/inspector.md) (F12 in development builds) shows
the element tree, roles and accessible names of a running window, which helps find a missing name before that check.

## Motion

Oriel has no custom animation: state changes take effect immediately. See
[Transitions](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/transitions.md) and
[Drawing and animation](https://github.com/egoist/mygo/blob/v0.3.6/docs/ui/drawing.md) for what the toolkit offers.

- **Default is none.** Do not add `Transition`, `Animate` or page transitions without a mockup that shows the change
  they explain and the maintainer's agreement; record the exception here.
- **If an exception is agreed,** use the built-ins with their defaults (element transitions are 200 ms, ease-out)
  and nothing longer than 300 ms for interface feedback. Avoid frame-by-frame custom animation.
- **Reduced motion.** `Animate` and element and page transitions already jump to the target when the desktop asks
  for less motion (`c.Preferences().ReduceMotion`). Any motion you compute yourself must read the preference.
  Spinners may keep turning: they show that work is going on.
- **Never animate to hide latency**, and do not loop decoration.
- The connect screen's only motion is its button's spinner icon while it connects. The timeline has none: new
  entries do not move the list; a "N new" pill appears at the top instead, and choosing it reloads and scrolls to
  the top.

## Localization

- The UI is in English and Simplified Chinese only. Every user-visible string is in the embedded catalog `copy.json`
  next to the screen, keyed `en` and `zh-Hans`. Change both together; do not add other languages.
- `locale.Language` (in `internal/ui/locale/`) maps the system locale: every Chinese locale, Traditional included,
  reads Simplified Chinese; everything else reads English. Screens use it rather than their own mapping.
- Every key has both translations, and each screen's test fails on a missing or untranslated entry. Accessible
  names are copy too: translate `Label` and `Description`.
- Feed content (titles, text, author names) is shown as it comes, in whatever language it is in; it is not copy.
  Translation of content is the on-demand AI feature, not localization.
- English labels: short imperatives ("Reset", "Add subscription"), sentence case, no trailing period on labels and a
  full stop on sentences. Chinese sentences use full-width punctuation. Do not concatenate sentence fragments, since
  word order differs; use full-sentence strings with placeholders.
- Layouts must tolerate the longer of the two translations, and English text 30–50% longer than the first draft.
  Do not fix widths of text controls.
- Format dates, times, numbers and plurals for the language. Chinese has no plural forms; English needs separate
  strings for one and many where it matters.

## Design acceptance checklist

Run before a UI pull request is merged, and on each target platform before a release:

- [ ] Every state in the mockup is reached in the app (for the connect screen: each state of the mockup's switcher).
- [ ] Both languages, light and dark, the default and the minimum window size checked.
- [ ] Keyboard-only run-through, and a screen reader pass on each target platform.
- [ ] High contrast and larger text size checked.
- [ ] Screenshots of each state, light and dark, are in the pull request body.

## References

This design language draws on the UI documentation of MyGo (MIT License, Copyright (c) egoist):
<https://github.com/egoist/mygo/tree/v0.3.6/docs/ui>, together with `docs/native.md`, `docs/menus.md` and
`docs/windows.md` from the same version. The text here is a summary in our own words; the upstream pages are the
source for APIs and behavior. See also [THIRD_PARTY.md](../THIRD_PARTY.md).
