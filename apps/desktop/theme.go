package main

import (
	"math"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// The colors and surfaces follow Kero (github.com/egoist/kero, MIT, ©
// EGOIST): its palettes, its themes ("washes") and its elevation. Views
// get colors with paletteFor(c) and never read raw mygo theme colors, so a
// theme changes one place.

// palette is the colors of the app in one appearance.
type palette struct {
	dark bool

	backdrop    ui.Color // under the sidebar where no material shows
	text        ui.Color
	textMuted   ui.Color // subtitles, times
	textFaint   ui.Color // hints, counts
	rowHover    ui.Color
	rowSelected ui.Color
	rowShadow   ui.Color

	card        ui.Color // the page's card
	cardShadow  ui.Color
	cardShadow2 ui.Color
	floatShadow ui.Color
	divider     ui.Color
	chip        ui.Color // quiet fills: code blocks, empty icons

	// Controls: light ones stand on a soft shadow, dark ones on a lit edge.
	control       ui.Color
	controlHover  ui.Color
	controlShadow bool

	accent    ui.Color
	success   ui.Color
	attention ui.Color // connecting, needs a look
	danger    ui.Color
	dangerBg  ui.Color
	done      ui.Color // the filled check of an unread job
	onDone    ui.Color
	onAccent  ui.Color // text on accent, success and danger badges

	overlay ui.Color // behind dialogs
	popover ui.Color // the palette and dialogs
	kbd     ui.Color // a shortcut's key cap
}

var lightPalette = palette{
	backdrop:    ui.Hex("#eceef4"),
	text:        ui.Hex("#1d1d1f"),
	textMuted:   ui.Hex("#6e6e78"),
	textFaint:   ui.Hex("#a3a3ad"),
	rowHover:    ui.RGBA(0, 0, 0, 0.04),
	rowSelected: ui.Hex("#ffffff"),
	rowShadow:   ui.RGBA(20, 20, 50, 0.08),

	card:        ui.Hex("#ffffff"),
	cardShadow:  ui.RGBA(0, 0, 0, 0.07),
	cardShadow2: ui.RGBA(0, 0, 0, 0.035),
	floatShadow: ui.RGBA(20, 20, 40, 0.14),
	divider:     ui.RGBA(0, 0, 0, 0.06),
	chip:        ui.RGBA(0, 0, 0, 0.045),

	control:       ui.Hex("#ffffff"),
	controlHover:  ui.Hex("#f7f7f9"),
	controlShadow: true,

	accent:    ui.Hex("#3b6cf6"),
	success:   ui.Hex("#22863a"),
	attention: ui.Hex("#c27b0f"),
	danger:    ui.Hex("#d93a35"),
	dangerBg:  ui.RGBA(217, 58, 53, 0.1),
	done:      ui.Hex("#1d1d1f"),
	onDone:    ui.Hex("#ffffff"),
	onAccent:  ui.Hex("#ffffff"),

	overlay: ui.RGBA(10, 10, 30, 0.12),
	popover: ui.Hex("#ffffff"),
	kbd:     ui.RGBA(0, 0, 0, 0.04),
}

var darkPalette = palette{
	dark:        true,
	backdrop:    ui.Hex("#141418"),
	text:        ui.Hex("#ececf0"),
	textMuted:   ui.Hex("#a9a9b4"),
	textFaint:   ui.Hex("#83838f"),
	rowHover:    ui.RGBA(255, 255, 255, 0.05),
	rowSelected: ui.RGBA(255, 255, 255, 0.10),
	rowShadow:   ui.RGBA(0, 0, 0, 0.25),

	card:        ui.Hex("#232328"),
	cardShadow:  ui.RGBA(0, 0, 0, 0.35),
	cardShadow2: ui.RGBA(0, 0, 0, 0.2),
	floatShadow: ui.RGBA(0, 0, 0, 0.45),
	divider:     ui.RGBA(255, 255, 255, 0.07),
	chip:        ui.RGBA(255, 255, 255, 0.07),

	control:      ui.Hex("#404047"),
	controlHover: ui.Hex("#4a4a52"),

	accent:    ui.Hex("#6b93ff"),
	success:   ui.Hex("#4fd17a"),
	attention: ui.Hex("#f0a33a"),
	danger:    ui.Hex("#ff6961"),
	dangerBg:  ui.RGBA(255, 105, 97, 0.14),
	done:      ui.Hex("#ececf0"),
	onDone:    ui.Hex("#161618"),
	onAccent:  ui.Hex("#161618"),

	overlay: ui.RGBA(0, 0, 0, 0.35),
	popover: ui.Hex("#232327"),
	kbd:     ui.RGBA(255, 255, 255, 0.06),
}

// --- themes ---

// A look is a theme in one appearance: its palette, and the wash under
// the sidebar and around the card (a gradient from top to bottom, with a
// glow from the top right).
type look struct {
	pal        *palette
	base, glow ui.LinearGradient
	plain      bool // the window's material under the sidebar's color
}

// defaultTheme is Kero's default.
const defaultTheme = "paper"

// themeNames are the themes, in the order the Settings page lists them.
var themeNames = []string{"paper", "mist", "meadow", "dusk", "tide", "apricot", "aurora", "dune", "petal", "plain"}

// currentTheme is the theme the window draws. The view sets it from the
// prefs; paletteFor reads it.
var currentTheme atomic.Pointer[string]

func setTheme(name string) {
	if _, ok := looks[name]; !ok {
		name = defaultTheme
	}
	currentTheme.Store(&name)
}

// lookFor is the look of the window c draws, by its theme and appearance.
func lookFor(c *ui.Context) *look {
	name := defaultTheme
	if p := currentTheme.Load(); p != nil {
		name = *p
	}
	l := looks[name]
	if c.Theme().Dark {
		return l[1]
	}
	return l[0]
}

// paletteFor is the palette of the window c draws.
func paletteFor(c *ui.Context) *palette { return lookFor(c).pal }

// backdrop paints the wash: the base on the root, the glow on row, which
// holds the rest. Over the window's material (vibrancy) it lets a little
// of the desktop show through.
func backdrop(c *ui.Context, row ui.Element) {
	l := lookFor(c)
	if l.plain {
		bg := l.pal.backdrop
		if c.Vibrancy() {
			bg = bg.Alpha(0.82)
		}
		c.Root().Background(bg)
		return
	}
	base := l.base
	if c.Vibrancy() {
		stops := make([]ui.GradientStop, len(base.Stops))
		for i, st := range base.Stops {
			stops[i] = ui.GradientStop{Color: st.Color.Alpha(0.95), At: st.At}
		}
		base.Stops = stops
	}
	c.Root().LinearGradient(base)
	row.LinearGradient(l.glow)
}

// tone is the hue of a theme, how much of it the card and text take, and
// the hue of its accent.
type tone struct{ hue, tint, accent float32 }

type wash struct {
	light, dark         []string
	lightGlow, darkGlow ui.Color
	tone                tone
}

// washes are Kero's themes. "petal" is Kero's "rex".
var washes = map[string]wash{
	"petal": {
		light: []string{"#ebe0f3", "#f3e7f1", "#f7eee4", "#f6efcf"}, lightGlow: ui.RGBA(247, 220, 241, 0.75),
		dark: []string{"#240f1d", "#26111a", "#271610"}, darkGlow: ui.RGBA(142, 58, 110, 0.3),
		tone: tone{350, 1, 350},
	},
	"mist": {
		light: []string{"#e3e9f4", "#ebeff6", "#f1f2f3", "#f2f1ec"}, lightGlow: ui.RGBA(210, 225, 248, 0.75),
		dark: []string{"#0c1628", "#0d1827", "#111b23"}, darkGlow: ui.RGBA(46, 91, 166, 0.3),
		tone: tone{255, 1, 255},
	},
	"meadow": {
		light: []string{"#e2eee5", "#ebf2e7", "#f3f2e2", "#f5f0d6"}, lightGlow: ui.RGBA(214, 240, 222, 0.75),
		dark: []string{"#081c0d", "#101d0c", "#1b1c0b"}, darkGlow: ui.RGBA(6, 113, 50, 0.3),
		tone: tone{140, 1, 140},
	},
	"dusk": {
		light: []string{"#e4dbf3", "#efdeee", "#f6e3dc", "#f7e6cf"}, lightGlow: ui.RGBA(246, 206, 222, 0.8),
		dark: []string{"#190f2e", "#260f24", "#2d140b"}, darkGlow: ui.RGBA(125, 65, 138, 0.3),
		tone: tone{315, 1, 315},
	},
	"tide": {
		light: []string{"#dbebef", "#e3eff1", "#ecf3f0", "#f2f2e8"}, lightGlow: ui.RGBA(194, 232, 238, 0.75),
		dark: []string{"#001b22", "#001e1f", "#0b1e1b"}, darkGlow: ui.RGBA(0, 112, 128, 0.3),
		tone: tone{205, 1, 205},
	},
	"apricot": {
		light: []string{"#f5e1d6", "#f8e7dc", "#f8eedf", "#f6f0da"}, lightGlow: ui.RGBA(252, 212, 194, 0.78),
		dark: []string{"#2a1207", "#291507", "#25190a"}, darkGlow: ui.RGBA(151, 62, 22, 0.3),
		tone: tone{50, 1, 50},
	},
	"aurora": {
		light: []string{"#dbefe8", "#e1ebf2", "#e8e4f4", "#f0e6f1"}, lightGlow: ui.RGBA(198, 238, 224, 0.75),
		dark: []string{"#001f13", "#06192b", "#1e122a"}, darkGlow: ui.RGBA(0, 115, 75, 0.3),
		tone: tone{170, 1, 170},
	},
	"dune": {
		light: []string{"#ede4d8", "#f1e9de", "#f4eee4", "#f3efe8"}, lightGlow: ui.RGBA(242, 222, 194, 0.7),
		dark: []string{"#22190b", "#211a0e", "#201b12"}, darkGlow: ui.RGBA(135, 79, 0, 0.3),
		tone: tone{80, 0.7, 80},
	},
	// White paper on a desk of light gray, in ink, flat and quiet.
	"paper": {
		light: []string{"#f6f6f7", "#f7f7f8", "#f8f8f8"}, lightGlow: ui.RGBA(255, 255, 255, 0),
		dark: []string{"#141416", "#151517", "#161618"}, darkGlow: ui.RGBA(90, 120, 200, 0.1),
		tone: tone{260, 0, 262},
	},
	"plain": {
		light: []string{"#eceef4", "#eceef4"}, lightGlow: ui.RGBA(0, 0, 0, 0),
		dark: []string{"#141418", "#141418"}, darkGlow: ui.RGBA(0, 0, 0, 0),
	},
}

// looks are the themes' looks, light and dark, made once.
var looks = func() map[string][2]*look {
	m := map[string][2]*look{}
	for name, w := range washes {
		var both [2]*look
		for i, dark := range []bool{false, true} {
			hexes, glow := w.light, w.lightGlow
			if dark {
				hexes, glow = w.dark, w.darkGlow
			}
			stops := make([]ui.GradientStop, len(hexes))
			for j, h := range hexes {
				stops[j] = ui.GradientStop{Color: ui.Hex(h), At: float32(j) / float32(len(hexes)-1)}
			}
			l := &look{
				base:  ui.LinearGradient{Angle: 180, Oklab: true, Stops: stops},
				glow:  ui.LinearGradient{Angle: 225, Oklab: true, Stops: []ui.GradientStop{{Color: glow, At: 0}, {Color: glow.Alpha(0), At: 0.5}}},
				plain: name == "plain",
			}
			if l.plain {
				l.pal = &lightPalette
				if dark {
					l.pal = &darkPalette
				}
			} else {
				l.pal = tinted(toneColors(w.tone, dark))
			}
			both[i] = l
		}
		m[name] = both
	}
	return m
}()

// toneColors are a theme's card, text and accent: the card and text
// tinted with its hue, the accent in its accent hue.
func toneColors(t tone, dark bool) (bg, fg, accent ui.Color) {
	tint := func(l, c float32) ui.Color { return ui.Oklch(l, c*t.tint, t.hue) }
	if dark {
		return tint(0.255, 0.02), tint(0.92, 0.015), ui.Oklch(0.74, 0.13, t.accent)
	}
	// The less tinted, the whiter the paper: an untinted theme's card is white.
	paper := 0.978 + 0.022*max(0, 1-t.tint/0.5)
	return tint(paper, 0.012), tint(0.28, 0.03), ui.Oklch(0.55, 0.13, t.accent)
}

// tinted is the light or dark palette, as bg is, in a theme's colors.
func tinted(bg, fg, accent ui.Color) *palette {
	p := lightPalette
	if l, _, _ := oklch(bg); l < 0.5 {
		p = darkPalette
	}
	white := ui.Hex("#ffffff")
	p.card = bg
	p.text, p.textMuted, p.textFaint = fg, fg.Mix(bg, 0.38), fg.Mix(bg, 0.56)
	p.done, p.onDone = fg, bg
	p.accent = accent
	if p.dark {
		p.popover = bg.Mix(white, 0.04)
	} else {
		p.rowSelected = bg.Mix(white, 0.5)
		p.popover = bg.Mix(white, 0.6)
	}
	return &p
}

// oklch returns a color's lightness, chroma and hue, as CSS's oklch().
func oklch(c ui.Color) (l, ch, h float32) {
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, b := lin(c.R), lin(c.G), lin(c.B)
	lc := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	mc := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	sc := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	L := 0.2104542553*lc + 0.7936177850*mc - 0.0040720468*sc
	A := 1.9779984951*lc - 2.4285922050*mc + 0.4505937099*sc
	B := 0.0259040371*lc + 0.7827717662*mc - 0.8086757660*sc
	hue := math.Mod(math.Atan2(B, A)*180/math.Pi+360, 360)
	return float32(L), float32(math.Hypot(A, B)), float32(hue)
}

// themed is the mygo theme in the app's colors, so the stock widgets
// (forms, selects, lists) match the palette. As Kero's, controls have no
// outline: they stand out by their fill.
func themed(c *ui.Context, pal *palette) *ui.Theme {
	t := *c.Theme()
	t.Background = pal.card
	t.Text = pal.text
	t.TextMuted = pal.textMuted
	t.Accent = pal.accent
	t.AccentText = ui.Hex("#ffffff")
	t.Selection = pal.accent.Alpha(0.25)
	t.Border = pal.divider
	t.Surface = pal.control
	t.SurfaceHover = pal.controlHover
	t.SurfacePressed = pal.controlHover.Mix(pal.text, 0.06)
	t.Success = pal.success
	t.Warning = pal.attention
	t.Danger = pal.danger
	return &t
}

// --- elevation ---

// raised lifts a surface off what is under it.
func raised(e ui.Element, pal *palette) ui.Element {
	if pal.dark {
		return darkShadow(litEdge(e, pal))
	}
	return e.Shadow(0, 1, 2, 0, pal.cardShadow).Shadow(0, 1, 6, 0, pal.cardShadow2)
}

// paned edges the page's card on the wash: in the light a hairline and no
// shadow; in the dark, as raised.
func paned(e ui.Element, pal *palette) ui.Element {
	if pal.dark {
		return raised(e, pal)
	}
	return e.Border(0.5, ui.RGBA(0, 0, 0, 0.106)).InsetShadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.063))
}

// chosen lifts the chosen row of the sidebar a hair.
func chosen(e ui.Element, pal *palette) ui.Element {
	if pal.dark {
		return raised(e, pal)
	}
	return e.Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.06)).Shadow(0, 0.5, 2, 0, ui.RGBA(0, 0, 0, 0.05))
}

// floating lifts a surface above the window: the palette, dialogs.
func floating(e ui.Element, pal *palette) ui.Element {
	if pal.dark {
		return darkShadow(litEdge(e, pal)).Shadow(0, 16, 40, 0, pal.floatShadow)
	}
	return e.Shadow(0, 1, 3, 0, pal.cardShadow).Shadow(0, 16, 40, 0, pal.floatShadow)
}

// litEdge draws a dark surface's edge as if lit from above: a border in
// the text color, 18% at the top fading to 6% at the bottom.
func litEdge(e ui.Element, pal *palette) ui.Element {
	return e.Border(1, pal.text.Alpha(0.12)).BorderLinearGradient(ui.LinearGradient{
		Angle: 180, From: pal.text.Alpha(0.18), To: pal.text.Alpha(0.06),
	})
}

// darkShadow is a surface's shadow in the dark: three tight layers.
func darkShadow(e ui.Element) ui.Element {
	return e.Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 2, 2, -1, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 4, 6, -3, ui.RGBA(0, 0, 0, 0.24))
}
