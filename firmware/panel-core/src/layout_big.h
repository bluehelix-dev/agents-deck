#pragma once
// The big panel layout: the Flutter desk panel's UI (clients/flutter/
// agents_ui: AgentsApp, AgentTile, DeckGrid, agent_detail.dart) drawn at a
// scale. The desk panel is a 4" 720 x 720 screen; a board of the same size
// at 480 x 480 gets every Flutter size times 2/3 (specBig480()), with the
// nearest bitmap fonts and whole-number critter scales. A 720 x 720 board
// would use k = 1 and larger fonts.
//
//   home    the deck's 4 x 4 grid of square tiles by slot, paged, with the
//           page bars under it (AgentTile, DeckGrid, _PageBars)
//   card    the Flutter overview: the critter (and its subagent crew), the
//           title, the tool chip and status pill; doing now; context with
//           the tick bar; tokens out, cache read, cost, turns; the last
//           prompt; model · folder · branch; and the button row: focus
//           terminal, interrupt while running, otherwise stop ("tap again to
//           stop" arms it for 3 s) or resume and remove once exited
//   prompt  one layout for permissions and questions (the Flutter question
//           card): the question, the detail, the options with their hint
//           lines; tap one to choose it, "send" answers. A permission is the
//           orange card, a question dark with the orange border.
//   states  waiting for approval, connecting, deck offline, no agents; on
//           Wi-Fi, where it is (joining, connecting, wrong password, network
//           not found, deck not reachable, certificate rejected)
//   Wi-Fi   on Wi-Fi a small mark at the right of the page bars' row, which
//           then stays with one page too
//
// Back: swipe right, or BOOT. Pure C++ (no Arduino).
#include <math.h>
#include <stdio.h>
#include <string.h>

#include "canvas.h"
#include "deck_cmd.h"
#include "deck_ui.h"
#include "font_big.h"
#include "font_data.h"
#include "model.h"
#include "panel_layout.h"
#include "round_geom.h"
#include "sprites.h"
#include "view.h"

struct BigSpec {
  const char* name;
  int w, h;
  float k;                     // Flutter pixels to board pixels
  // Fonts for the Flutter sizes (at k): 30 bold, 22-24 bold, 19-20, 16-18, 13-15, 12.5.
  const Font *title, *heading, *body, *text, *label, *tileTitle, *tileMeta;
  int tileScale, subScale, cardScale, crewScale, noticeScale;
};

// A 4" 480 x 480 board: the desk panel at 2/3.
static inline BigSpec specBig480() {
  BigSpec s{};
  s.name = "big480";
  s.w = 480; s.h = 480; s.k = 480.f / 720.f;
  s.title = &FONT_B20;       // rm 30 bold  -> 20
  s.heading = &FONT_B15;     // rm 22-24 bold -> 15-16
  s.body = &FONT_M13;        // rm 20, buttons 19 -> 13
  s.text = &FONT_S;          // rm 17, mono 16-18 -> 11-12
  s.label = &FONT_XS;        // mono 13-15 -> 9-10
  s.tileTitle = &FONT_S;     // mono 16 -> 10.7
  s.tileMeta = &FONT_S;      // mono 12.5 -> 8.3; 11.7 so it reads from the desk (with the tight status line)
  s.tileScale = 3;           // 92 px on a 170 px cell -> 61 px: x3 is the nearest (54)
  s.subScale = 1;            // 36 px -> 24 px
  s.cardScale = 2;           // 54 px -> 36 px: exactly x2
  s.crewScale = 1;           // 36 px -> 24 px
  s.noticeScale = 5;         // 126 px -> 84 px: x5 (90)
  return s;
}

// A Flutter length at the spec's scale.
static inline int bk(const BigSpec& S, float v) { return (int)lroundf(v * S.k); }

// ---------------------------------------------------------------- text

// A font's size in pixels (Roboto Mono advances 0.6 em).
static inline int fontPx(const Font& f) { return (int)lroundf(f.cellW / 0.6f); }

// Text whose em box (Flutter's height-1.0 line) starts at `top`.
static inline int textAt(Canvas& c, const Font& f, int x, int top, const char* s, uint16_t col, int maxW = -1) {
  return c.text(f, x, top - (f.cellH - fontPx(f)) / 2, s, col, maxW);
}

// ---------------------------------------------------------------- the grid

static inline int bigPages(const Fleet& f) { return f.count ? f.agents[f.count - 1].slot / 16 + 1 : 1; }

struct BigGrid {
  int x0, y0, cell, cellH, gap;
  Rect bars;                   // w = 0 with one page
};

// DeckGrid: square cells with an 8 px gap inside 8 px padding; with more than
// one page the bars take 34 px at the bottom and the cells get shorter. On
// Wi-Fi (wifi) that row stays with one page too: the Wi-Fi mark sits in it.
static inline BigGrid bigGrid(const BigSpec& S, int pages, bool wifi = false) {
  BigGrid g{};
  const int pad = bk(S, 8), barsH = pages > 1 || wifi ? bk(S, 34) : 0;
  g.gap = bk(S, 8);
  const int availW = S.w - 2 * pad, availH = S.h - pad - (barsH ? 0 : pad) - barsH;
  g.cell = (availW - 3 * g.gap) / 4;
  g.cellH = (availH - 3 * g.gap) / 4;
  if (pages < 2 && g.cellH > g.cell) g.cellH = g.cell;
  if (g.cell > g.cellH && pages < 2) g.cell = g.cellH;
  const int gw = 4 * g.cell + 3 * g.gap, gh = 4 * g.cellH + 3 * g.gap;
  g.x0 = (S.w - gw) / 2;
  g.y0 = pad + (availH - gh) / 2;
  g.bars = barsH ? Rect{0, S.h - barsH, S.w, barsH} : Rect{0, 0, 0, 0};
  return g;
}

static inline Rect bigCell(const BigGrid& g, int cell) {
  return Rect{g.x0 + (cell % 4) * (g.cell + g.gap), g.y0 + (cell / 4) * (g.cellH + g.gap), g.cell, g.cellH};
}

static inline int bigAgentAt(const Fleet& f, int page, int cell) {
  if (cell < 0 || cell >= 16) return -1;
  for (int i = 0; i < f.count; i++)
    if (f.agents[i].slot == page * 16 + cell) return i;
  return -1;
}

// ---------------------------------------------------------------- the card's parts

// The button row (AgentButton): 68 px tall, 14 px apart, at the card's foot.
struct BigButtons {
  int n = 0;
  Hit kind[3];
  Rect r[3];
};

static inline Rect bigCardInner(const BigSpec& S) {
  return Rect{bk(S, 36), bk(S, 34), S.w - 2 * bk(S, 36), S.h - bk(S, 34) - bk(S, 30)};
}

// Focus only where the deck can bring a terminal to the front (Server.focus:
// not on Windows); without it the other button takes the row.
static inline BigButtons bigCardButtons(const BigSpec& S, const Agent& a, DeckScope scope, bool canFocus = true) {
  BigButtons b;
  const Rect in = bigCardInner(S);
  const int h = bk(S, 68), y = in.y + in.h - h, gap = bk(S, 14), side = bk(S, 200);
  const bool exited = a.status == Status::Exited, running = a.status == Status::Running;
  // What the scope allows: focus and interrupt need act, stop / remove / resume admin.
  const bool act = canAct(scope), admin = scope == DeckScope::Admin;
  if (!act) return b;
  if (!exited) {
    const bool second = running || admin;
    if (canFocus) {
      b.kind[b.n] = Hit::Focus;
      b.r[b.n++] = Rect{in.x, y, second ? in.w - gap - side : in.w, h};
    }
    const Rect rest = canFocus ? Rect{in.x + in.w - side, y, side, h} : Rect{in.x, y, in.w, h};
    if (running) {
      b.kind[b.n] = Hit::Interrupt;
      b.r[b.n++] = rest;
    } else if (admin) {
      b.kind[b.n] = Hit::Stop;
      b.r[b.n++] = rest;
    }
  } else if (admin && a.resumable) {
    b.kind[b.n] = Hit::Resume;
    b.r[b.n++] = Rect{in.x, y, in.w - gap - side, h};
    b.kind[b.n] = Hit::Stop;
    b.r[b.n++] = Rect{in.x + in.w - side, y, side, h};
  } else if (admin) {
    b.kind[b.n] = Hit::Stop;
    b.r[b.n++] = Rect{in.x, y, in.w, h};
  }
  return b;
}

// The prompt: the question, the detail box, the context line, the options
// (pinned above the button row), and send / focus terminal.
struct BigPrompt {
  int titleY, titleLines;
  Rect box;                    // h = 0: no detail
  int boxLines;
  int ctxY;                    // 0: no context line
  int n;
  Rect opt[kMaxOptions];
  bool subs;                   // room for the hint lines
  Rect send, terminal;
};

static inline int bigWrapLines(const Font& f, const char* s, int maxW, int maxLines) {
  Canvas none{nullptr, 0, 0};
  return textWrapped(none, f, 0, 0, s, 0, maxW, maxLines);
}

static inline void bigSplit(const char* label, char* title, size_t tcap, char* sub, size_t scap) {
  const char* seps[] = {" \xE2\x80\x94 ", " \xE2\x80\x93 ", " - "};
  for (const char* sep : seps) {
    const char* p = strstr(label, sep);
    if (!p) continue;
    size_t n = (size_t)(p - label);
    if (n >= tcap) n = tcap - 1;
    memcpy(title, label, n);
    title[n] = '\0';
    snprintf(sub, scap, "%s", p + strlen(sep));
    return;
  }
  snprintf(title, tcap, "%s", label);
  sub[0] = '\0';
}

static inline int bigTopBarBottom(const BigSpec& S) {
  return bk(S, 34) + 13 * S.cardScale + bk(S, 10) + bk(S, 30);
}

static inline BigPrompt bigPrompt(const BigSpec& S, const Agent& a, bool canFocus = true) {
  BigPrompt p{};
  const bool perm = !strcmp(a.waitKind, "permission");
  const Rect in = bigCardInner(S);
  const int btnH = bk(S, 68), btnY = in.y + in.h - btnH;
  // send, and focus terminal beside it where the deck can focus.
  p.send = canFocus ? Rect{in.x, btnY, in.w - bk(S, 14) - bk(S, 260), btnH} : Rect{in.x, btnY, in.w, btnH};
  p.terminal = canFocus ? Rect{in.x + in.w - bk(S, 260), btnY, bk(S, 260), btnH} : Rect{0, 0, 0, 0};
  const int qLine = (int)lroundf(fontPx(*S.heading) * 1.3f), tLine = (int)lroundf(fontPx(*S.text) * 1.45f);
  p.titleY = bigTopBarBottom(S) + bk(S, 20);
  p.titleLines = bigWrapLines(*S.heading, a.waitTitle[0] ? a.waitTitle : "needs you", in.w, 3);
  if (p.titleLines < 1) p.titleLines = 1;
  int y = p.titleY + p.titleLines * qLine;
  // The options, from the bottom up: 74 px rows 10 apart, shorter (and
  // without hints) when there are many.
  p.n = a.optCount;
  const int bottom = btnY - bk(S, 20);
  int rowH = bk(S, 74);
  const int gap = bk(S, 10);
  const int textTop = y + bk(S, 10);
  if (p.n) {
    const int room = bottom - textTop - fontPx(*S.label) - bk(S, 10);
    const int fit = (room - (p.n - 1) * gap) / p.n;
    if (fit < rowH) rowH = fit < fontPx(*S.text) + 8 ? fontPx(*S.text) + 8 : fit;
  }
  p.subs = rowH >= fontPx(*S.text) + fontPx(*S.label) + bk(S, 4) + 2 * bk(S, 10);
  // The lowest the options may start (pinned above the buttons); they move up
  // under the detail when there is room, so the choice sits by what it is about.
  const int lowest = p.n ? bottom - p.n * rowH - (p.n - 1) * gap : bottom;
  int optTop = lowest;
  // The detail in a box, with the lines left above the options.
  if (a.waitDetail[0]) {
    const int pad = bk(S, 16);
    const int ctxH = a.promptCtx[0] && perm ? fontPx(*S.label) + bk(S, 10) : 0;
    int lines = (optTop - bk(S, 10) - ctxH - textTop - 2 * pad) / tLine;
    if (lines > 4) lines = 4;
    const int need = bigWrapLines(*S.text, a.waitDetail, in.w - 2 * bk(S, 18), lines > 0 ? lines : 1);
    if (need < lines) lines = need;
    if (lines >= 1) {
      p.boxLines = lines;
      p.box = Rect{in.x, textTop, in.w, lines * tLine + 2 * pad};
      y = p.box.y + p.box.h;
    }
  }
  // Where a permission runs (a question's context is in its pill).
  if (perm && a.promptCtx[0] && y + bk(S, 10) + fontPx(*S.label) <= optTop - 4) {
    p.ctxY = y + bk(S, 10);
    y = p.ctxY + fontPx(*S.label);
  }
  const int under = (p.box.h || p.ctxY ? y : textTop - bk(S, 10)) + bk(S, 20);
  if (under < optTop) optTop = under;
  for (int i = 0; i < p.n; i++) p.opt[i] = Rect{in.x, optTop + i * (rowH + gap), in.w, rowH};
  return p;
}

// The option chosen to start with: a question's primary; none on a permission,
// so a quick tap on send never approves by itself.
static inline int bigDefaultOption(const Agent& a) {
  if (!strcmp(a.waitKind, "permission")) return -1;
  for (int i = 0; i < a.optCount; i++)
    if (a.opts[i].primary) return i;
  return -1;
}

// ---------------------------------------------------------------- hit-testing and touch

static inline PanelHit bigHit(const BigSpec& S, const PanelUi& ui, const Fleet& f, int x, int y, DeckScope scope) {
  PanelHit h;
  if (ui.screen == Screen::Home) {
    const int pages = bigPages(f);
    const BigGrid g = bigGrid(S, pages, ui.wifi);
    if (pages > 1 && g.bars.contains(x, y)) { h.kind = Hit::PageBar; h.index = x * pages / S.w; return h; }
    for (int cell = 0; cell < 16; cell++)
      if (bigCell(g, cell).contains(x, y)) {
        const int i = bigAgentAt(f, ui.page < pages ? ui.page : 0, cell);
        if (i >= 0) { h.kind = Hit::Tile; h.agent = i; }
        return h;
      }
    return h;
  }
  const int i = findAgent(f, ui.openId);
  if (i < 0) return h;
  const Agent& a = f.agents[i];
  h.agent = i;
  if (ui.screen == Screen::Card) {
    const BigButtons b = bigCardButtons(S, a, scope, f.canFocus);
    for (int k = 0; k < b.n; k++)
      if (b.r[k].contains(x, y)) { h.kind = b.kind[k]; return h; }
    return PanelHit();
  }
  const BigPrompt p = bigPrompt(S, a, f.canFocus);
  for (int k = 0; k < p.n; k++)
    if (p.opt[k].contains(x, y)) { h.kind = Hit::Option; h.index = k; return h; }
  if (p.send.contains(x, y)) { h.kind = Hit::Send; return h; }
  if (p.terminal.w && p.terminal.contains(x, y)) { h.kind = Hit::Focus; return h; }
  return PanelHit();
}

constexpr uint32_t kStopArmMs = 3000;   // "tap again to stop" holds this long

static inline void bigOpen(PanelUi& ui, const Fleet& f, int i) {
  const Agent& a = f.agents[i];
  snprintf(ui.openId, sizeof ui.openId, "%s", a.id);
  ui.screen = a.status == Status::Waiting ? Screen::Prompt : Screen::Card;
  ui.selected = bigDefaultOption(a);
  ui.stopArmedUntil = 0;
  ui.backAfterToast = false;
}

static inline void bigHome(PanelUi& ui) {
  ui.screen = Screen::Home;
  ui.openId[0] = '\0';
  ui.backAfterToast = false;
  ui.stopArmedUntil = 0;
}

static inline DeckCommand bigTap(PanelUi& ui, const Fleet& f, const PanelHit& h, DeckScope scope, uint32_t ms) {
  DeckCommand out;
  switch (h.kind) {
    case Hit::Tile: bigOpen(ui, f, h.agent); break;
    case Hit::PageBar: ui.page = h.index; break;
    case Hit::Focus:
      if (!canAct(scope)) { uiToast(ui, "view only: needs scope act", ms); break; }
      out = commandFor(DeckCmd::Focus, f.agents[h.agent]);
      uiToast(ui, "focused", ms, 1200);
      break;
    case Hit::Interrupt:
      out = commandFor(DeckCmd::Interrupt, f.agents[h.agent]);
      uiToast(ui, "sent", ms, 1200);
      break;
    case Hit::Resume:
      out = commandFor(DeckCmd::Resume, f.agents[h.agent]);
      uiToast(ui, "resuming", ms, 1200);
      break;
    case Hit::Stop:
      if (f.agents[h.agent].status == Status::Exited || (int32_t)(ui.stopArmedUntil - ms) > 0) {
        out = commandFor(DeckCmd::Dismiss, f.agents[h.agent]);
        uiToast(ui, f.agents[h.agent].status == Status::Exited ? "removed" : "stopping\xE2\x80\xA6", ms, 1500);
        ui.stopArmedUntil = 0;
      } else {
        ui.stopArmedUntil = ms + kStopArmMs;   // first tap: arm
      }
      break;
    case Hit::Option: ui.selected = h.index; break;
    case Hit::Send: {
      const Agent& a = f.agents[h.agent];
      if (ui.selected < 0 || ui.selected >= a.optCount || ui.backAfterToast) break;
      if (!canAct(scope)) { uiToast(ui, "view only: needs scope act", ms); break; }
      out = commandFor(DeckCmd::Answer, a);
      snprintf(out.prompt, sizeof out.prompt, "%s", a.promptId);
      snprintf(out.key, sizeof out.key, "%s", a.opts[ui.selected].key);
      uiToast(ui, "sent", ms, 1000);
      ui.backAfterToast = true;
      break;
    }
    default: break;
  }
  return out;
}

static inline DeckCommand bigTouch(PanelUi& ui, const BigSpec& S, const Fleet& f, bool touching, int x, int y,
                                   DeckScope scope, uint32_t ms) {
  DeckCommand out;
  constexpr int slop = 14, swipeMin = 40;
  if (touching) {
    if (!ui.down) {
      ui.down = true;
      ui.x0 = ui.x = x;
      ui.y0 = ui.y = y;
      ui.downMs = ms;
      ui.pressed = bigHit(S, ui, f, x, y, scope);
      return out;
    }
    ui.x = x;
    ui.y = y;
    const int dx = x - ui.x0, dy = y - ui.y0;
    if (dx * dx + dy * dy > slop * slop) ui.pressed = PanelHit();   // it became a swipe
    return out;
  }
  if (!ui.down) return out;
  ui.down = false;
  const PanelHit pressed = ui.pressed;
  ui.pressed = PanelHit();
  const int dx = ui.x - ui.x0, dy = ui.y - ui.y0;
  const int adx = dx < 0 ? -dx : dx, ady = dy < 0 ? -dy : dy;
  if (adx <= slop && ady <= slop) {
    if (pressed.kind != Hit::None) out = bigTap(ui, f, pressed, scope, ms);
  } else if (adx >= swipeMin || ady >= swipeMin) {
    const bool horizontal = adx >= ady;
    if (ui.screen != Screen::Home) {
      if (horizontal && dx > 0) bigHome(ui);
    } else {
      const int pages = bigPages(f);
      const bool next = horizontal ? dx < 0 : dy < 0;
      if (next && ui.page < pages - 1) ui.page++;
      else if (!next && ui.page > 0) ui.page--;
    }
  }
  return out;
}

static inline bool bigTick(PanelUi& ui, const Fleet& f, uint32_t ms) {
  const int pages = bigPages(f);
  if (ui.page >= pages) ui.page = pages - 1;
  if (ui.page < 0) ui.page = 0;
  if (ui.screen != Screen::Home) {
    const int i = findAgent(f, ui.openId);
    if (i < 0) bigHome(ui);
    else if (ui.screen == Screen::Prompt && f.agents[i].status != Status::Waiting && !ui.backAfterToast)
      ui.screen = Screen::Card;
    else if (ui.screen == Screen::Card && f.agents[i].status == Status::Waiting) {
      ui.screen = Screen::Prompt;
      ui.selected = bigDefaultOption(f.agents[i]);
    }
  }
  if (ui.backAfterToast && (int32_t)(ui.toastUntil - ms) <= 0) bigHome(ui);
  if (ui.stopArmedUntil && (int32_t)(ui.stopArmedUntil - ms) <= 0) ui.stopArmedUntil = 0;
  return false;
}

static inline void bigResult(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) {
  if (!seq || seq != ui.cmdSeq) return;
  ui.cmdSeq = 0;
  if (code == 204 || code == 200) return;
  if (code == 403) uiToast(ui, "not allowed: scope", ms);
  else if (code == 409) uiToast(ui, "the prompt changed", ms);
  else {
    char b[24];
    snprintf(b, sizeof b, "error %u", (unsigned)code);
    uiToast(ui, b, ms);
  }
  ui.backAfterToast = false;
}

static inline void bigButton(PanelUi& ui, const Fleet& f) {
  if (ui.screen != Screen::Home) { bigHome(ui); return; }
  ui.page = (ui.page + 1) % bigPages(f);
}

// ---------------------------------------------------------------- drawing

namespace big_detail {

constexpr uint16_t kRowOutline = rgb565(0x2A, 0x29, 0x25);   // agentsRowOutline
constexpr uint16_t kRowSubOnInk = rgb565(0x3B, 0x3A, 0x36);  // agentsRowSubOnInk

static inline void rrect(Canvas& c, const Rect& r, int rad, uint16_t col) {
  if (rad * 2 > r.h) rad = r.h / 2;
  if (rad * 2 > r.w) rad = r.w / 2;
  if (rad <= 0) { c.rect(r.x, r.y, r.w, r.h, col); return; }
  c.rect(r.x + rad, r.y, r.w - 2 * rad, r.h, col);
  c.rect(r.x, r.y + rad, rad, r.h - 2 * rad, col);
  c.rect(r.x + r.w - rad, r.y + rad, rad, r.h - 2 * rad, col);
  fillArc(c, r.x + rad, r.y + rad, 0, rad, 270, 90, col);
  fillArc(c, r.x + r.w - rad, r.y + rad, 0, rad, 0, 90, col);
  fillArc(c, r.x + r.w - rad, r.y + r.h - rad, 0, rad, 90, 90, col);
  fillArc(c, r.x + rad, r.y + r.h - rad, 0, rad, 180, 90, col);
}

// A rounded outline `t` px thick, inside the box.
static inline void rframe(Canvas& c, const Rect& r, int rad, int t, uint16_t col) {
  if (rad * 2 > r.h) rad = r.h / 2;
  c.rect(r.x + rad, r.y, r.w - 2 * rad, t, col);
  c.rect(r.x + rad, r.y + r.h - t, r.w - 2 * rad, t, col);
  c.rect(r.x, r.y + rad, t, r.h - 2 * rad, col);
  c.rect(r.x + r.w - t, r.y + rad, t, r.h - 2 * rad, col);
  fillArc(c, r.x + rad, r.y + rad, rad - t, rad, 270, 90, col);
  fillArc(c, r.x + r.w - rad, r.y + rad, rad - t, rad, 0, 90, col);
  fillArc(c, r.x + r.w - rad, r.y + r.h - rad, rad - t, rad, 90, 90, col);
  fillArc(c, r.x + rad, r.y + r.h - rad, rad - t, rad, 180, 90, col);
}

// AgentButton: 14 px radius, 2 px outline, the label centred (or left).
static inline void button(Canvas& c, const BigSpec& S, const Rect& r, const char* label, uint16_t fill, uint16_t ink,
                          uint16_t outline, bool left = false) {
  rrect(c, r, bk(S, 14), outline);
  if (fill != outline) rrect(c, Rect{r.x + 2, r.y + 2, r.w - 4, r.h - 4}, bk(S, 14) - 2, fill);
  const int pad = bk(S, 22);
  // The biggest of body / text / label that fits.
  const Font* fonts[] = {S.body, S.text, S.label};
  const Font* f = S.label;
  for (const Font* g : fonts)
    if (textW(*g, label) <= r.w - 2 * pad) { f = g; break; }
  const int w = textW(*f, label) < r.w - 2 * pad ? textW(*f, label) : r.w - 2 * pad;
  const int x = left ? r.x + pad : r.x + (r.w - w) / 2;
  c.text(*f, x, r.y + (r.h - f->cellH) / 2 + 1, label, ink, r.w - 2 * pad);
}

// Diagonal accent stripes sliding across a hungry tile (HungryStripes).
static inline void stripes(Canvas& c, const Rect& r, uint16_t fill, uint16_t accent, uint32_t ms, int period) {
  const uint16_t s = blend565(fill, accent, 2);   // accent at 16 %
  const int shift = (int)((ms / 100) % 6) * period / 6;
  for (int yy = 0; yy < r.h; yy++)
    for (int xx = 0; xx < r.w; xx++) {
      const int u = ((xx + (r.h - yy) - shift) % period + period) % period;
      const int px = r.x + xx, py = r.y + yy;
      if (u < period / 2 && px >= 0 && px < c.w && py >= 0 && py < c.h) c.px[py * c.w + px] = s;
    }
}

// ContextRingPainter: `pct` of the border, clockwise from the top centre.
static inline void ring(Canvas& c, const Rect& r, int t, int pct, uint16_t color, uint16_t track) {
  c.frame(r.x, r.y, r.w, r.h, track, t);
  if (pct <= 0) return;
  const int W = r.w - t, H = r.h - t;
  int left = 2 * (W + H) * (pct > 100 ? 100 : pct) / 100;
  struct Seg { int x, y, dx, dy, len; } segs[] = {
      {r.x + W / 2, r.y, 1, 0, W - W / 2}, {r.x + W, r.y, 0, 1, H}, {r.x + W, r.y + H, -1, 0, W},
      {r.x, r.y + H, 0, -1, H},            {r.x, r.y, 1, 0, W / 2},
  };
  for (const Seg& s : segs) {
    if (left <= 0) break;
    const int n = left < s.len ? left : s.len;
    if (s.dx > 0) c.rect(s.x, s.y, n + t, t, color);
    else if (s.dx < 0) c.rect(s.x - n, s.y, n + t, t, color);
    else if (s.dy > 0) c.rect(s.x, s.y, t, n + t, color);
    else c.rect(s.x, s.y - n, t, n + t, color);
    left -= n;
  }
}

// A small hash for the twinkle's pixels (and a string's seed).
static inline uint32_t mix32(uint32_t h) {
  h ^= h >> 16; h *= 0x7feb352dU; h ^= h >> 15; h *= 0x846ca68bU; h ^= h >> 16;
  return h;
}
static inline uint32_t seedOf(const char* s) {
  uint32_t h = 2166136261u;
  for (; *s; s++) { h ^= (uint8_t)*s; h *= 16777619u; }
  return h;
}

// WorkflowTwinkle: a dim grid of blue pixels in cells of `cell` px, and about
// a sixth of them, at places fixed by `seed`, fading up and down on their own
// clocks (2.4 to 4.8 s): up in four steps over 40 % of the cycle, lit to 60 %,
// down in four.
static inline void twinkle(Canvas& c, const Rect& r, float cell, uint16_t fill, uint16_t blue, uint32_t seed, uint32_t ms) {
  const int cols = (int)(r.w / cell), rows = (int)(r.h / cell);
  if (cols < 1 || rows < 1) return;
  int side = (int)lroundf(cell * 0.3f);
  if (side < 1) side = 1;
  static const uint8_t kAlpha[] = {2, 2, 3, 4, 5};   // of 15: the grid at ~13 %, a lit pixel up to ~33 %
  for (int y = 0; y < rows; y++)
    for (int x = 0; x < cols; x++) {
      const uint32_t h = mix32(seed ^ ((uint32_t)(y * 977 + x) * 0x9E3779B9u));
      int step = 0;
      if (h % 400 < 70) {
        const uint32_t dur = 2400 + (h >> 9) % 2400, ph = (h >> 3) % dur;
        const float f = (float)((ms + ph) % dur) / (float)dur;
        step = f < 0.4f ? (int)(f / 0.4f * 4) : f < 0.6f ? 4 : 4 - (int)((f - 0.6f) / 0.4f * 4);
        if (step < 0) step = 0;
        if (step > 4) step = 4;
      }
      c.rect(r.x + (int)(x * cell), r.y + (int)(y * cell), side, side, blend565(fill, blue, kAlpha[step]));
    }
}

// WorkflowBar: one segment per phase, the running one lit in the workflow's
// blue, done ones at 45 %, the rest in the tile's dim at 35 %.
static inline void workflowBar(Canvas& c, const Theme& T, const Agent& a, int x, int y, int w, int h, uint16_t fill,
                               uint16_t dim, int gap) {
  if (!a.wfCount) return;
  const int i = wfCurrent(a), n = a.wfCount;
  const int seg = (w - (n - 1) * gap) / n;
  for (int k = 0; k < n; k++) {
    const uint16_t col = k == i ? T.workflow
                         : (k < i || (a.wf[k].done && !a.wf[k].running)) ? blend565(fill, T.workflow, 7)
                                                                           : blend565(fill, dim, 5);
    c.rect(x + k * (seg + gap), y, k == n - 1 ? w - k * (seg + gap) : seg, h, col);
  }
}

// AgentTile, laid out on the deck's 170 px design cell and scaled to the real one.
static inline void tile(Canvas& c, const BigSpec& S, const Theme& T, const Agent& a, const Rect& r, uint32_t ms,
                        int64_t now, bool pressed) {
  Look k = lookOf(T, a);
  const uint16_t fill = pressed ? T.ink : k.fill;
  auto cc = [&](uint16_t v) { return pressed ? T.bg : v; };
  const int side = r.w < r.h ? r.w : r.h;
  const float u = side / 170.f;                     // design px -> board px
  auto d = [&](float v) { return (int)lroundf(v * u); };
  const int ox = r.x + (r.w - side) / 2, oy = r.y + (r.h - side) / 2;   // the FittedBox's square
  c.rect(r.x, r.y, r.w, r.h, fill);
  if (k.stripes && !pressed) stripes(c, r, fill, T.accent, ms, d(24));
  else if (workflowing(a) && !pressed) twinkle(c, r, side / 20.f, fill, T.workflow, seedOf(a.id), ms);
  // SubagentLadder: up the right edge, the newest on top; four fit, a "+N"
  // above them counts the rest. The session steps left to make room.
  int ladder = 0;
  if (a.subagents) {
    ladder = d(36 + 2);
    const int s = S.subScale, n = a.subCount ? (a.subCount < 4 ? a.subCount : 4) : 1;
    const int more = a.subagents - n;
    const int x = r.x + r.w - d(2) - 18 * s;
    int bottom = r.y + r.h - d(8);
    for (int i = 0; i < n; i++) {                   // from the bottom up: the oldest shown first
      const int idx = a.subCount ? (a.subCount - n) + i : -1;
      const char* tool = idx >= 0 ? a.subs[idx].tool : a.subTool;
      const Pose sp = tool[0] ? poseOfTool(tool) : Pose::Think;
      drawSprite(c, x, bottom - 8 * s, s, Species::Claude, sp, cc(k.body), fill, ms, true, a.hat, a.hatColor);
      bottom -= 13 * s + d(3);
    }
    if (more > 0) {
      char m[8];
      snprintf(m, sizeof m, "+%d", more);
      c.text(*S.label, r.x + r.w - d(2) - d(8) - textW(*S.label, m), bottom + d(3) - d(2) - S.label->cellH, m, cc(k.meta));
    }
  }
  const int cw = side - ladder;                     // the column the session is centred in
  const int cx = ox + cw / 2;
  // The critter: its body where the deck draws it (92 px wide from y 30; the
  // feet at 30 + 12/13 of its height), at the nearest whole scale. The flag of
  // a finished turn is waved in orange, as for work.
  const int s = S.tileScale;
  const float unit = 92.f / 18.f;                    // the deck's critter: px per sprite unit
  const int feet = oy + d(30 + 12 * unit);
  const Pose pose = poseOf(a, now);
  const uint16_t body = pose == Pose::Celebrate ? T.accent : k.body;
  drawSprite(c, cx - 7 * s, feet - 12 * s, s, a.species, pose, cc(body), fill, ms, true, a.hat, a.hatColor);
  // Title (mono 16) at 30 + 92 x 13/18 + 14; then the status line, or a
  // running workflow's bar and line in its blue.
  const int titleTop = oy + d(30 + 92.f * 13 / 18 + 14);
  const int tw = cw - 2 * d(10);
  auto centred = [&](const Font& f, int top, const char* t, uint16_t col) {
    const int w = textW(f, t) < tw ? textW(f, t) : (tw / f.cellW) * f.cellW;
    textAt(c, f, cx - w / 2, top, t, col, tw);
  };
  centred(*S.tileTitle, titleTop, a.title[0] ? a.title : a.id, cc(k.label));
  int y = titleTop + fontPx(*S.tileTitle) + d(5);
  char meta[80];
  if (a.wfCount) {
    const int bw = d(104) < tw ? d(104) : tw, bh = d(5) < 2 ? 2 : d(5);
    workflowBar(c, T, a, cx - bw / 2, y, bw, bh, fill, cc(k.meta), d(3) < 1 ? 1 : d(3));
    y += bh + d(5);
    wfLine(a, meta, sizeof meta);
    centred(*S.tileMeta, y, meta, cc(T.workflow));
  } else {
    metaLine(a, now, meta, sizeof meta, true);
    centred(*S.tileMeta, y, meta, cc(k.meta));
  }
  if (!pressed) ring(c, r, d(4) < 2 ? 2 : d(4), a.ctxWindow ? ctxPct(a) : 0, k.ring, k.ringTrack);
}

static inline void home(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in) {
  const Fleet& f = *in.fleet;
  const PanelUi& ui = *in.ui;
  const int pages = bigPages(f);
  const int page = ui.page < pages ? ui.page : pages - 1;
  const BigGrid g = bigGrid(S, pages, ui.wifi);
  for (int cell = 0; cell < 16; cell++) {
    const Rect r = bigCell(g, cell);
    const int i = bigAgentAt(f, page, cell);
    if (i >= 0) tile(c, S, T, f.agents[i], r, in.ms, in.nowEpoch, ui.pressed.kind == Hit::Tile && ui.pressed.agent == i);
    else c.rect(r.x, r.y, r.w, r.h, T.emptyFill);   // a free cell: nearly the background, no outline
  }
  if (ui.wifi && g.bars.w)                           // the Wi-Fi mark (round_geom.h), right-aligned with the grid
    drawNetMark(c, (float)(g.x0 + 4 * g.cell + 3 * g.gap - 18), (float)(g.bars.y + g.bars.h / 2 + 6), T.dim);
  if (pages > 1) {                                   // _PageBars
    bool hot[64] = {};
    for (int i = 0; i < f.count; i++)
      if (f.agents[i].status == Status::Waiting && f.agents[i].slot / 16 < 64) hot[f.agents[i].slot / 16] = true;
    const int cur = bk(S, 56), other = bk(S, 30), h = bk(S, 8), pad = bk(S, 6);
    int total = 0;
    for (int p = 0; p < pages; p++) total += (p == page ? cur : other) + 2 * pad;
    int x = (S.w - total) / 2 + pad;
    const int y = g.bars.y + (g.bars.h - h) / 2;
    for (int p = 0; p < pages; p++) {
      const int w = p == page ? cur : other;
      rrect(c, Rect{x, y, w, h}, bk(S, 4), p == page ? T.ink : (p < 64 && hot[p]) ? T.accent : kRowOutline);
      x += w + 2 * pad;
    }
  }
  if (in.link == LinkState::Lost) {                  // _OfflineChip, 22 px off the bottom
    const char* s = "offline" DECK_DOT "retrying";
    const Font& f2 = *S.label;
    const int w = textW(f2, s) + 2 * bk(S, 18), h = fontPx(f2) + 2 * bk(S, 9);
    const Rect r{(S.w - w) / 2, S.h - bk(S, 22) - h, w, h};
    rrect(c, r, h / 2, T.accent);
    rrect(c, Rect{r.x + 1, r.y + 1, r.w - 2, r.h - 2}, h / 2 - 1, T.panel);
    textAt(c, f2, r.x + bk(S, 18), r.y + bk(S, 9), s, T.accent);
  }
}

// "84k", "3.1m" (compactCount).
static inline void compact(uint64_t n, char* out, size_t cap) {
  if (n < 1000) { snprintf(out, cap, "%u", (unsigned)n); return; }
  const double v = n < 999500 ? n / 1000.0 : n / 1000000.0;
  const char* unit = n < 999500 ? "k" : "m";
  if (v < 10) {
    snprintf(out, cap, "%.1f%s", v, unit);
    char* dot = strstr(out, ".0");
    if (dot) memmove(dot, dot + 2, strlen(dot + 2) + 1);
  } else {
    snprintf(out, cap, "%d%s", (int)(v + 0.5), unit);
  }
}

// agentPillLabel.
static inline void pillLabel(const Agent& a, int64_t now, char* out, size_t cap) {
  char t[8];
  fmtAge(a.statusSince ? now - a.statusSince : 0, t, sizeof t);
  switch (a.status) {
    case Status::Running:  snprintf(out, cap, "running" DECK_DOT "%s", t); break;
    case Status::Waiting:
      if (!strcmp(a.waitKind, "question") && a.promptCtx[0]) snprintf(out, cap, "%s", a.promptCtx);
      else snprintf(out, cap, "needs you" DECK_DOT "%s", t);
      break;
    case Status::Idle:     snprintf(out, cap, "%s" DECK_DOT "%s", a.unseen ? "ready" : "idle", t); break;
    case Status::Error:    snprintf(out, cap, "error"); break;
    case Status::Starting: snprintf(out, cap, "starting" DECK_DOT "%s", t); break;
    case Status::Exited:   snprintf(out, cap, "%s" DECK_DOT "%s", a.lost ? "lost with tmux" : "exited", t); break;
    default:               snprintf(out, cap, "idle" DECK_DOT "%s", t);
  }
}

// _topBar: the critter and its crew, the title under them; on the right, the
// tool chip and the pill level with the title.
static inline void topBar(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a, uint16_t ink,
                          uint16_t pillFill, uint16_t pillInk, uint16_t body, uint16_t cut) {
  const Rect r = bigCardInner(S);
  const int cs = S.cardScale;
  // Its subagents are listed under the header (crewList); the flag of a
  // finished turn is waved in orange, as for work.
  const Pose pose = poseOf(a, in.nowEpoch);
  drawSprite(c, r.x, r.y, cs, a.species, pose, pose == Pose::Celebrate ? T.accent : body, cut, in.ms, true, a.hat,
             a.hatColor);
  const int titleTop = r.y + 13 * cs + bk(S, 10), titleH = bk(S, 30);
  // The pill (mono 15, 12 x 4 padding, radius 8) and the chip, right-aligned.
  char pill[112];
  pillLabel(a, in.nowEpoch, pill, sizeof pill);
  const Font& pf = *S.label;
  const int ph = fontPx(pf) + 2 * bk(S, 4), maxPw = bk(S, 300);
  int pw = textW(pf, pill) + 2 * bk(S, 12);
  if (pw > maxPw) pw = maxPw;
  const Rect pr{r.x + r.w - pw, titleTop + (titleH - ph) / 2, pw, ph};
  rrect(c, pr, bk(S, 8), pillFill);
  textAt(c, pf, pr.x + bk(S, 12), pr.y + bk(S, 4), pill, pillInk, pw - 2 * bk(S, 12));
  textAt(c, *S.title, r.x, titleTop + (titleH - fontPx(*S.title)) / 2, a.title[0] ? a.title : a.id, ink,
         pr.x - bk(S, 16) - r.x);
}

static inline void tickBar(Canvas& c, const BigSpec& S, int x, int y, int w, int pct, uint16_t col) {
  const int h = bk(S, 14), mid = y + h / 2, pitch = bk(S, 6) < 3 ? 3 : bk(S, 6);
  const int half = bk(S, 3.5f) < 2 ? 2 : bk(S, 3.5f), thin = bk(S, 1.6f) < 1 ? 1 : bk(S, 1.6f);
  for (int xx = 0; xx < w; xx += pitch) c.rect(x + xx, mid - half, thin, 2 * half + 1, col);
  if (pct > 0) {
    const int thick = bk(S, 7);
    c.rect(x, mid - thick / 2, w * (pct > 100 ? 100 : pct) / 100, thick, col);
  }
}

// The card's subagents (SubagentList): a line each with its critter, what it
// was sent to do, its kind (a workflow agent's phase) and its tool with how
// long it has been at it ("Grep · 2m"); as many lines as `rows`, then a count.
static inline int crewList(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a, int x,
                           int y, int w, int rows, uint16_t body) {
  const Font& lf = *S.label;
  char buf[96];
  const bool wf = a.subCount && !strcmp(a.subs[0].type, "workflow");
  snprintf(buf, sizeof buf, wf ? "workflow" DECK_DOT "%u agent%s" : "%u subagent%s", (unsigned)a.subagents,
           a.subagents == 1 ? "" : "s");
  textAt(c, lf, x, y, buf, T.dim);
  y += fontPx(lf) + bk(S, 8);
  const int rowH = bk(S, 36), toolW = bk(S, 128), kindW = w >= bk(S, 420) ? bk(S, 110) : 0;
  const int ss = S.crewScale;
  for (int i = 0; i < rows && i < a.subCount; i++) {
    const Subagent& s = a.subs[i];
    drawSprite(c, x, y + (rowH - 13 * ss) / 2 + 5 * ss - 2, ss, Species::Claude,
               s.tool[0] ? poseOfTool(s.tool) : Pose::Think, body, T.bg, in.ms, true, a.hat, a.hatColor);
    // "label · phase" for a workflow agent; "general-purpose" says "general".
    char title[48], kind[24];
    snprintf(title, sizeof title, "%s", s.title);
    snprintf(kind, sizeof kind, "%s", !strcmp(s.type, "general-purpose") ? "general" : s.type);
    if (!strcmp(s.type, "workflow")) {
      char* sep = strstr(title, DECK_DOT);
      for (char* p = sep; p; p = strstr(p + 1, DECK_DOT)) sep = p;
      if (sep) {
        snprintf(kind, sizeof kind, "%s", sep + strlen(DECK_DOT));
        *sep = '\0';
      } else {
        snprintf(kind, sizeof kind, "workflow");
      }
    }
    char tool[32], age[8];
    if (s.startedAt && in.nowEpoch) {
      fmtAge(in.nowEpoch - s.startedAt, age, sizeof age);
      snprintf(tool, sizeof tool, "%s" DECK_DOT "%s", s.tool[0] ? s.tool : "thinking", age);
    } else {
      snprintf(tool, sizeof tool, "%s", s.tool[0] ? s.tool : "thinking");
    }
    const int tx = x + 18 * ss + bk(S, 18), ty = y + (rowH - fontPx(*S.text)) / 2;
    const int toolX = x + w - toolW, kindX = toolX - kindW;
    textAt(c, *S.text, tx, ty, title, T.ink, kindX - bk(S, 12) - tx);
    if (kindW) textAt(c, lf, kindX, ty + fontPx(*S.text) - fontPx(lf), kind, T.dim, kindW - bk(S, 8));
    const int tw = textW(lf, tool) < toolW ? textW(lf, tool) : toolW;
    textAt(c, lf, x + w - tw, ty + fontPx(*S.text) - fontPx(lf), tool, T.dim, toolW);
    y += rowH;
  }
  const int shown = rows < a.subCount ? rows : a.subCount;
  if (a.subagents > shown && shown > 0) {
    snprintf(buf, sizeof buf, "+%u more", (unsigned)(a.subagents - shown));
    textAt(c, lf, x, y + bk(S, 4), buf, T.dim);
    y += fontPx(lf) + bk(S, 8);
  }
  return y;
}

// WorkflowPhases: each phase with its count, the running one in the
// workflow's blue, done ones in ink, the ones to come dim.
static inline void workflowPhases(Canvas& c, const BigSpec& S, const Theme& T, const Agent& a, int x, int y, int w) {
  const Font& f = *S.body;
  const int cur = wfCurrent(a), gap = bk(S, 18);
  int xx = x;
  char buf[32];
  for (int k = 0; k < a.wfCount; k++) {
    const WorkflowPhase& p = a.wf[k];
    snprintf(buf, sizeof buf, "%s %u/%u", p.title, (unsigned)p.done, (unsigned)(p.done + p.running));
    const int bw = textW(f, buf);
    if (xx + bw > x + w) {                           // no room for the next: say there are more
      if (xx + textW(f, "\xE2\x80\xA6") <= x + w) textAt(c, f, xx, y, "\xE2\x80\xA6", T.dim);
      return;
    }
    const uint16_t col = k == cur ? T.workflow : (k < cur || (p.done && !p.running)) ? T.ink : T.dim;
    textAt(c, f, xx, y, buf, col);
    xx += bw + gap;
  }
}

static inline void card(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  const Rect r = bigCardInner(S);
  char buf[224];
  const Look look = lookOf(T, a);
  // A finished turn nobody has looked at slides its stripes behind the card
  // too, and a workflow at work twinkles there, as on its tile.
  if (hungry(a)) stripes(c, Rect{0, 0, S.w, S.h}, T.bg, T.accent, in.ms, bk(S, 24));
  else if (workflowing(a)) twinkle(c, Rect{0, 0, S.w, S.h}, (float)bk(S, 14), T.bg, T.workflow, seedOf(a.id), in.ms);
  uint16_t pillFill = T.panel, pillInk = T.ink;
  if (a.status == Status::Running) { pillFill = T.ink; pillInk = T.bg; }
  else if (a.status == Status::Error) { pillFill = T.accent; pillInk = T.bg; }
  const uint16_t body = look.body == T.bg ? T.ink : look.body;
  topBar(c, S, T, in, a, T.ink, pillFill, pillInk, body, T.bg);
  // The sections' spacing; tighter when a workflow and its crew share the card.
  const int G = a.wfCount && a.subagents ? bk(S, 16) : bk(S, 26);
  int y = bigTopBarBottom(S) + G;
  const Font& lf = *S.label;
  const BigButtons b = bigCardButtons(S, a, in.scope, in.fleet->canFocus);
  const int btnTop = b.n ? b.r[0].y : r.y + r.h;
  const int footerY = btnTop - (b.n ? bk(S, 22) : 0) - fontPx(lf);
  const int line = (int)lroundf(fontPx(*S.text) * 1.45f);
  // A running workflow: its name and phases.
  if (a.wfCount) {
    snprintf(buf, sizeof buf, "workflow" DECK_DOT "%s", a.wfName);
    textAt(c, lf, r.x, y, buf, T.dim, r.w);
    y += fontPx(lf) + bk(S, 8);
    workflowPhases(c, S, T, a, r.x, y, r.w);
    y += fontPx(*S.body) + G;
  }
  // Its subagents, in the room the rest leaves (one line of the last prompt).
  if (a.subagents) {
    const int rest = (fontPx(lf) + bk(S, 8) + fontPx(*S.body) + G)        // doing now
                     + (fontPx(*S.heading) + bk(S, 10) + bk(S, 14) + G)   // context
                     + (fontPx(lf) + bk(S, 6) + fontPx(*S.heading) + G)   // tokens, cost, turns
                     + (fontPx(lf) + bk(S, 8) + line);                            // the last prompt, one line
    int room = footerY - bk(S, 8) - y - rest - (fontPx(lf) + bk(S, 8)) - G;
    const int rowH = bk(S, 36), moreH = fontPx(lf) + bk(S, 8);
    const int kept = a.subCount;
    int rows = room / rowH;
    if (rows > 4) rows = 4;
    if (rows >= kept && a.subagents <= kept) rows = kept;
    else rows = (room - moreH) / rowH < kept ? (room - moreH) / rowH : kept;
    if (rows < 0) rows = 0;
    y = crewList(c, S, T, in, a, r.x, y, r.w, rows, body) + G - bk(S, 8);
  }
  // Doing now (_doingNow).
  const char* label = "doing now";
  uint16_t valCol = T.ink;
  char val[160];
  switch (a.status) {
    case Status::Running:
      if (!a.actTool[0]) snprintf(val, sizeof val, "thinking");
      else {
        char verb[80];
        metaLine(a, 0, verb, sizeof verb);
        char* dot = strstr(verb, DECK_DOT);
        if (dot) *dot = '\0';
        snprintf(val, sizeof val, "%s%s%s", verb, a.actDetail[0] ? DECK_DOT : "", a.actDetail);
      }
      break;
    case Status::Error:
      label = "error";
      snprintf(val, sizeof val, "%s", a.errMsg[0] ? a.errMsg : "error");
      valCol = T.accent;
      break;
    case Status::Starting:
      label = "starting";
      snprintf(val, sizeof val, "launching agent\xE2\x80\xA6");
      break;
    default: {
      label = "session";
      const char* t = a.aiTitle[0] ? a.aiTitle : a.folder;
      snprintf(val, sizeof val, "%s", t[0] ? t : "\xE2\x80\x94");
      for (char* p = val; *p; p++) *p = (char)tolower((unsigned char)*p);
    }
  }
  textAt(c, lf, r.x, y, label, T.dim);
  y += fontPx(lf) + bk(S, 8);
  textAt(c, *S.body, r.x, y, val, valCol, r.w);
  y += fontPx(*S.body) + G;
  // Context.
  const Font& hf = *S.heading;
  const int cw = textAt(c, hf, r.x, y, "context", T.ink);
  if (a.ctxWindow) {
    char u[16], w[16];
    compact(a.ctxUsed, u, sizeof u);
    compact(a.ctxWindow, w, sizeof w);
    snprintf(buf, sizeof buf, "%s / %s", u, w);
  } else {
    snprintf(buf, sizeof buf, "--");
  }
  textAt(c, lf, r.x + cw + bk(S, 12), y + fontPx(hf) - fontPx(lf) - 1, buf, T.dim);
  snprintf(buf, sizeof buf, a.ctxWindow ? "%d%%" : "--", ctxPct(a));
  textAt(c, hf, r.x + r.w - textW(hf, buf), y, buf, T.ink);
  y += fontPx(hf) + bk(S, 10);
  tickBar(c, S, r.x, y, r.w, a.ctxWindow ? ctxPct(a) : 0, T.ink);
  y += bk(S, 14) + G;
  // Tokens out, cache read, cost, turns.
  {
    const char* labels[4] = {"tokens out", "cache read", "cost" DECK_DOT "est", "turns"};
    char vals[4][16];
    compact(a.tokensOut, vals[0], sizeof vals[0]);
    compact(a.cacheRead, vals[1], sizeof vals[1]);
    if (a.tool == ToolKind::Codex || a.costUsd <= 0) snprintf(vals[2], sizeof vals[2], "\xE2\x80\x94");
    else snprintf(vals[2], sizeof vals[2], "$%.2f", (double)a.costUsd);
    snprintf(vals[3], sizeof vals[3], "%u", a.turns);
    const int colW = r.w / 4;
    for (int i = 0; i < 4; i++) {
      textAt(c, lf, r.x + i * colW, y, labels[i], T.dim, colW - 4);
      textAt(c, hf, r.x + i * colW, y + fontPx(lf) + bk(S, 6), vals[i], T.ink, colW - 4);
    }
    y += fontPx(lf) + bk(S, 6) + fontPx(hf) + G;
  }
  // The last prompt, up to four lines, down to the footer.
  textAt(c, lf, r.x, y, "last prompt", T.dim);
  y += fontPx(lf) + bk(S, 8);
  int lines = (footerY - bk(S, 8) - y) / line;
  if (lines > 4) lines = 4;
  if (lines > 0)
    textWrapped(c, *S.text, r.x, y - (S.text->cellH - fontPx(*S.text)) / 2, a.lastPrompt[0] ? a.lastPrompt : "\xE2\x80\x94",
                T.ink, r.w, lines, line);
  // Footer: model · folder · branch, and the deck's name.
  {
    buf[0] = '\0';
    const char* parts[3] = {a.model, a.folder, a.branch};
    for (const char* p : parts) {
      if (!p[0]) continue;
      if (buf[0]) strncat(buf, DECK_DOT, sizeof buf - strlen(buf) - 1);
      strncat(buf, p, sizeof buf - strlen(buf) - 1);
    }
    const char* server = in.fleet->server;
    const int sw = textW(lf, server);
    textAt(c, lf, r.x + r.w - sw, footerY, server, T.dim);
    textAt(c, lf, r.x, footerY, buf, T.dim, r.w - sw - bk(S, 16));
  }
  // The buttons.
  const PanelUi& ui = *in.ui;
  const bool armed = (int32_t)(ui.stopArmedUntil - in.ms) > 0;
  for (int i = 0; i < b.n; i++) {
    const bool pressed = ui.pressed.kind == b.kind[i];
    switch (b.kind[i]) {
      case Hit::Focus:
        if (pressed) button(c, S, b.r[i], "focus terminal", T.accent, T.bg, T.accent);
        else button(c, S, b.r[i], hungry(a) ? "review in terminal" : "focus terminal", T.ink, T.bg, T.ink);
        break;
      case Hit::Resume:
        button(c, S, b.r[i], "resume", pressed ? T.accent : T.ink, T.bg, pressed ? T.accent : T.ink);
        break;
      case Hit::Interrupt:
        if (pressed) button(c, S, b.r[i], "interrupt", T.ink, T.bg, T.ink);
        else button(c, S, b.r[i], "interrupt", T.bg, T.ink, T.dim);
        break;
      default: {                                     // stop / remove
        const bool exited = a.status == Status::Exited;
        const char* l = exited ? "remove" : armed ? "tap again to stop" : "stop";
        if (pressed || (armed && !exited)) button(c, S, b.r[i], l, T.accent, T.bg, T.accent);
        else button(c, S, b.r[i], l, T.bg, T.ink, T.dim);
      }
    }
  }
}

static inline void prompt(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in, const Agent& a) {
  const bool perm = !strcmp(a.waitKind, "permission");
  const uint16_t surface = perm ? T.accent : T.bg;
  const uint16_t ink = perm ? T.bg : T.ink;
  const int rad = bk(S, 26);
  if (perm) rrect(c, Rect{0, 0, S.w, S.h}, rad, T.accent);
  else rframe(c, Rect{0, 0, S.w, S.h}, rad, bk(S, 3), T.accent);
  topBar(c, S, T, in, a, ink, perm ? T.bg : T.accent, perm ? T.accent : T.bg, perm ? T.bg : T.accent, surface);
  const Rect r = bigCardInner(S);
  const bool canFocus = in.fleet->canFocus;
  const BigPrompt p = bigPrompt(S, a, canFocus);
  const int qLine = (int)lroundf(fontPx(*S.heading) * 1.3f), tLine = (int)lroundf(fontPx(*S.text) * 1.45f);
  textWrapped(c, *S.heading, r.x, p.titleY - (S.heading->cellH - fontPx(*S.heading)) / 2,
              a.waitTitle[0] ? a.waitTitle : "needs you", ink, r.w, p.titleLines, qLine);
  if (p.box.h) {
    rrect(c, p.box, bk(S, 12), perm ? T.bg : T.panel);
    textWrapped(c, *S.text, p.box.x + bk(S, 18), p.box.y + bk(S, 16) - (S.text->cellH - fontPx(*S.text)) / 2 +
                (tLine - fontPx(*S.text)) / 2, a.waitDetail, T.ink, p.box.w - 2 * bk(S, 18), p.boxLines, tLine);
  }
  if (p.ctxY) textAt(c, *S.label, r.x, p.ctxY, a.promptCtx, perm ? T.bg : T.dim, r.w);

  // The options (_questionRow): the key, the title and its hint.
  const PanelUi& ui = *in.ui;
  for (int i = 0; i < p.n; i++) {
    const Rect& o = p.opt[i];
    const bool on = ui.selected == i || (ui.pressed.kind == Hit::Option && ui.pressed.index == i);
    uint16_t fill, edge, fg, sub;
    if (perm) {
      fill = on ? T.bg : T.accent;
      edge = T.bg;
      fg = on ? T.accent : T.bg;
      sub = on ? T.dim : blend565(T.accent, T.bg, 9);
    } else {
      fill = on ? T.ink : T.panel;
      edge = on ? T.ink : kRowOutline;
      fg = on ? T.bg : T.ink;
      sub = on ? kRowSubOnInk : T.dim;
    }
    rrect(c, o, bk(S, 14), edge);
    rrect(c, Rect{o.x + (perm ? 2 : 1), o.y + (perm ? 2 : 1), o.w - (perm ? 4 : 2), o.h - (perm ? 4 : 2)},
          bk(S, 14) - (perm ? 2 : 1), fill);
    const int px = o.x + bk(S, 20);
    textAt(c, *S.heading, px, o.y + (o.h - fontPx(*S.heading)) / 2, a.opts[i].key, fg, bk(S, 22) + 4);
    char title[96], hint[96];
    bigSplit(a.opts[i].label, title, sizeof title, hint, sizeof hint);
    const int tx = px + bk(S, 22) + bk(S, 18), tw = o.x + o.w - bk(S, 20) - tx;
    if (p.subs && hint[0]) {
      const int block = fontPx(*S.text) + bk(S, 4) + fontPx(*S.label);
      const int ty = o.y + (o.h - block) / 2;
      textAt(c, *S.text, tx, ty, title, fg, tw);
      textAt(c, *S.label, tx, ty + fontPx(*S.text) + bk(S, 4), hint, sub, tw);
    } else {
      textAt(c, *S.text, tx, o.y + (o.h - fontPx(*S.text)) / 2, title, fg, tw);
    }
  }
  if (!p.n) textAt(c, *S.label, r.x, p.send.y - bk(S, 20) - fontPx(*S.label), "free-text answer" DECK_DOT "type it at the terminal",
                   perm ? T.bg : T.dim, r.w);

  // The answer button says what it does: on a permission "allow", "allow
  // always" or "deny" once one is chosen ("choose one" before); a question
  // "send N". Focus terminal beside it where the deck can focus.
  char send[32];
  const bool chosen = ui.selected >= 0 && ui.selected < a.optCount;
  if (chosen && perm) {
    const char* l = a.opts[ui.selected].label;
    if (!strncmp(l, "yes", 3)) snprintf(send, sizeof send, "%s", strstr(l, "don't ask") || strstr(l, "always") ? "allow always" : "allow");
    else if (!strncmp(l, "no", 2)) snprintf(send, sizeof send, "deny");
    else snprintf(send, sizeof send, "send %s", a.opts[ui.selected].key);
  } else if (chosen) {
    snprintf(send, sizeof send, "send %s", a.opts[ui.selected].key);
  } else {
    snprintf(send, sizeof send, perm ? "choose one" : "send");
  }
  const bool sp = ui.pressed.kind == Hit::Send, fp = ui.pressed.kind == Hit::Focus;
  if (p.n) {
    if (sp && chosen) button(c, S, p.send, send, T.ink, T.bg, T.ink);
    else if (!chosen) button(c, S, p.send, send, perm ? blend565(T.accent, T.bg, 4) : T.panel, perm ? T.bg : T.dim,
                             perm ? blend565(T.accent, T.bg, 4) : T.panel);
    else if (perm) button(c, S, p.send, send, T.bg, T.accent, T.bg);
    else button(c, S, p.send, send, T.accent, T.bg, T.accent);
  }
  if (!canFocus) return;
  const Rect term = p.n ? p.terminal : Rect{r.x, p.send.y, r.w, p.send.h};
  if (fp) button(c, S, term, "focus terminal", perm ? T.bg : T.ink, perm ? T.accent : T.bg, perm ? T.bg : T.ink);
  else button(c, S, term, "focus terminal", surface, ink, perm ? T.bg : T.dim);
}

// _Notice: a centred critter, a title, a few lines (one of them a command).
static inline void notice(Canvas& c, const BigSpec& S, const Theme& T, Pose pose, const char* title, const char* l1,
                          const char* code, const char* l2, uint32_t ms) {
  const int s = S.noticeScale;
  const Font& tf = *S.title;
  const Font& lf = *S.text;
  const Font& cf = *S.body;
  const int lines = (l1 ? 1 : 0) + (code ? 1 : 0) + (l2 ? 1 : 0);
  int y = S.h / 2 - (13 * s + bk(S, 28) + fontPx(tf) + lines * (bk(S, 12) + fontPx(cf))) / 2;
  drawSprite(c, S.w / 2 - 7 * s, y, s, Species::Claude, pose, T.dim, T.bg, ms, true);
  y += 13 * s + bk(S, 28);
  const int maxW = S.w - 2 * bk(S, 48);
  auto centred = [&](const Font& f, const char* t, uint16_t col) {
    const int w = textW(f, t) < maxW ? textW(f, t) : (maxW / f.cellW) * f.cellW;
    textAt(c, f, (S.w - w) / 2, y, t, col, maxW);
  };
  centred(tf, title, T.ink);
  y += fontPx(tf);
  if (l1) { y += bk(S, 12); centred(lf, l1, T.dim); y += fontPx(cf); }
  if (code) { y += bk(S, 12); centred(cf, code, T.accent); y += fontPx(cf); }
  if (l2) { y += bk(S, 12); centred(lf, l2, T.dim); }
}

// A toast: in the middle; on a notice, low (bottom = true), clear of its text.
static inline void toast(Canvas& c, const BigSpec& S, const Theme& T, const char* s, bool bottom = false) {
  const Font& f = *S.body;
  const int w = textW(f, s) + 2 * bk(S, 24), h = fontPx(f) + 2 * bk(S, 14);
  const Rect r{(S.w - w) / 2, bottom ? S.h - bk(S, 48) - h : (S.h - h) / 2, w, h};
  rrect(c, r, h / 2, T.ink);
  rrect(c, Rect{r.x + 2, r.y + 2, r.w - 4, r.h - 4}, h / 2 - 2, T.panel);
  textAt(c, f, r.x + bk(S, 24), r.y + bk(S, 14), s, T.ink);
}

}  // namespace big_detail

static inline void drawBig(Canvas& c, const BigSpec& S, const Theme& T, const PanelInput& in) {
  using namespace big_detail;
  c.fill(T.bg);
  char buf[96];
  const PanelUi* tu = in.ui && in.ui->toast[0] && (int32_t)(in.ui->toastUntil - in.ms) > 0 ? in.ui : nullptr;
  switch (in.link) {
    case LinkState::Connecting:
      if (in.net && in.net->via == NetVia::Wifi) {   // where Wi-Fi is (net_model.h's words); a problem in orange
        const NetNotice n = netNotice(*in.net);
        if (n.problem) notice(c, S, T, Pose::Exit, n.title, nullptr, n.line1, n.line2, in.ms);
        else notice(c, S, T, Pose::Think, n.title, n.line1, nullptr, n.line2, in.ms);
      } else {
        notice(c, S, T, Pose::Think, "connecting to the deck", "plugged into the Mac?", "agentctl link add <port>",
               nullptr, in.ms);
      }
      if (tu) toast(c, S, T, tu->toast, true);
      return;
    case LinkState::Unapproved:
      snprintf(buf, sizeof buf, "agentctl devices approve %s", in.device && in.device[0] ? in.device : "<name>");
      notice(c, S, T, Pose::Wait, "waiting for approval", "on the Mac, run", buf, nullptr, in.ms);
      if (tu) toast(c, S, T, tu->toast, true);
      return;
    default: break;
  }
  if (!in.fleet || in.fleet->count == 0) {
    if (in.link == LinkState::Lost) {
      char seen[8];
      fmtAge(in.silentSec, seen, sizeof seen);
      snprintf(buf, sizeof buf, "last seen %s ago" DECK_DOT "retrying\xE2\x80\xA6", seen);
      notice(c, S, T, Pose::Exit, "deck offline", buf, "agentctl serve", nullptr, in.ms);
    } else {
      notice(c, S, T, Pose::Idle, "no agents", in.fleet && in.fleet->server[0] ? in.fleet->server : nullptr,
             "agentctl new", nullptr, in.ms);
    }
    if (tu) toast(c, S, T, tu->toast, true);
    return;
  }
  const PanelUi& ui = *in.ui;
  const int open = ui.screen != Screen::Home ? findAgent(*in.fleet, ui.openId) : -1;
  if (open >= 0 && ui.screen == Screen::Card) card(c, S, T, in, in.fleet->agents[open]);
  else if (open >= 0) prompt(c, S, T, in, in.fleet->agents[open]);
  else home(c, S, T, in);
  if (tu) toast(c, S, T, ui.toast);
}

// The big panels' PanelLayout.
class BigPanel : public PanelLayout {
 public:
  explicit BigPanel(const BigSpec& s) : S(s), T(deckTheme(false)) {}
  int width() const override { return S.w; }
  int height() const override { return S.h; }
  bool trueBlack() const override { return false; }
  void draw(Canvas& c, const PanelInput& in) const override { drawBig(c, S, T, in); }
  DeckCommand touch(PanelUi& ui, const Fleet& f, bool down, int x, int y, DeckScope scope, uint32_t ms) const override {
    return bigTouch(ui, S, f, down, x, y, scope, ms);
  }
  bool tick(PanelUi& ui, const Fleet& f, uint32_t ms) const override { return bigTick(ui, f, ms); }
  void button(PanelUi& ui, const Fleet& f) const override { bigButton(ui, f); }
  void result(PanelUi& ui, uint32_t seq, uint32_t code, uint32_t ms) const override { bigResult(ui, seq, code, ms); }
  const BigSpec S;
  const Theme T;
};
