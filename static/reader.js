// Reader behaviour: theme toggle, reading progress, GA4 scroll milestones,
// the "Notes & Thoughts" comment form, and the hidden author tools.
// Loaded before Alpine.
(function () {
  var MILESTONES = [25, 50, 75, 90, 100];

  // GA4 events (sent only when GA is on: GA_MEASUREMENT_ID set, reader not
  // signed in as the author). GA itself records page_view (someone arrived)
  // and engagement time automatically.
  //   story_scroll_milestone {percent}        25/50/75/90/100% of the story seen
  //   story_completed        {active_seconds} reached the end (once per visit)
  //   story_time_spent       {seconds}        active reading time milestones
  //   story_comment_posted                    a reader left a note
  function track(name, params) {
    if (typeof window.gtag === "function") window.gtag("event", name, params);
  }

  // Active reading time: counts only while the tab is visible and focused,
  // so a tab left open in the background isn't counted as reading.
  var TIME_MARKS = [15, 30, 60, 120, 180, 300, 480, 600, 900];
  var activeSeconds = 0;
  var nextMark = 0;
  setInterval(function () {
    if (document.visibilityState !== "visible" || !document.hasFocus()) return;
    activeSeconds++;
    while (nextMark < TIME_MARKS.length && activeSeconds >= TIME_MARKS[nextMark]) {
      track("story_time_spent", { seconds: TIME_MARKS[nextMark] });
      nextMark++;
    }
  }, 1000);

  var GENERIC_ERROR = "Something went wrong. Please try again.";

  // postJSON resolves with the parsed body (null for 204) or rejects with an
  // Error carrying the server's message and the HTTP status.
  function postJSON(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    }).then(function (res) {
      if (res.ok) return res.status === 204 ? null : res.json();
      return res
        .json()
        .catch(function () {
          return {};
        })
        .then(function (b) {
          var err = new Error(b.error || GENERIC_ERROR);
          err.status = res.status;
          throw err;
        });
    });
  }

  // Relative timestamps ("2 hours ago"). The server renders a plain date as
  // the fallback; anything older than a week keeps that date.
  var rtf = window.Intl && Intl.RelativeTimeFormat ? new Intl.RelativeTimeFormat("en", { numeric: "auto" }) : null;
  var UNITS = [
    ["minute", 60],
    ["hour", 3600],
    ["day", 86400],
  ];

  function relative(date) {
    var secs = (Date.now() - date.getTime()) / 1000;
    if (!rtf || isNaN(secs) || secs > 7 * 86400) return null;
    if (secs < 45) return "just now";
    for (var i = UNITS.length - 1; i >= 0; i--) {
      if (secs >= UNITS[i][1]) return rtf.format(-Math.round(secs / UNITS[i][1]), UNITS[i][0]);
    }
    return rtf.format(-1, "minute");
  }

  function updateTimes(root) {
    var els = (root || document).querySelectorAll("time[data-relative]");
    for (var i = 0; i < els.length; i++) {
      var d = new Date(els[i].getAttribute("datetime"));
      var text = relative(d);
      if (text) els[i].textContent = text;
      els[i].title = d.toLocaleString();
    }
  }

  // Reader privacy. These are deterrents: no web page can truly stop OS
  // screenshots (phone buttons, Cmd+Shift+4, Snipping Tool) or a determined
  // copier (view-source, reader mode, JavaScript off). Active only when
  // <body> has .protected, i.e. for everyone except the signed-in author.
  function inField(el) {
    return !!(el && el.closest && el.closest("input, textarea, [contenteditable]"));
  }

  function initPrivacy() {
    if (!document.body.classList.contains("protected")) return;
    var root = document.documentElement;
    var shield = function (on) {
      root.classList.toggle("shielded", on);
    };

    // Copy, cut, right-click, drag and text selection (form fields excepted).
    ["copy", "cut", "contextmenu", "dragstart", "selectstart"].forEach(function (type) {
      document.addEventListener(type, function (e) {
        if (!inField(e.target)) e.preventDefault();
      });
    });

    // Save page, print, view-source; and copy / select-all outside fields.
    document.addEventListener("keydown", function (e) {
      if (!(e.ctrlKey || e.metaKey)) return;
      var k = (e.key || "").toLowerCase();
      if (k === "s" || k === "p" || k === "u") e.preventDefault();
      else if ((k === "c" || k === "x" || k === "a") && !inField(e.target)) e.preventDefault();
    });

    // Windows PrintScreen: the page only hears about it after the capture,
    // so overwrite the clipboard copy and briefly black out the page.
    document.addEventListener("keyup", function (e) {
      if (e.key !== "PrintScreen") return;
      shield(true);
      setTimeout(function () {
        shield(false);
      }, 1500);
      if (navigator.clipboard) navigator.clipboard.writeText("Screenshots of Dear Chaya are disabled.").catch(function () {});
    });

    // Black screen whenever the page is hidden (app switcher thumbnails, tab
    // previews) or loses focus (screen-capture tools that take focus first).
    document.addEventListener("visibilitychange", function () {
      shield(document.hidden);
    });
    window.addEventListener("blur", function () {
      shield(true);
    });
    window.addEventListener("focus", function () {
      shield(false);
    });
    window.addEventListener("beforeprint", function () {
      shield(true);
    });
    window.addEventListener("afterprint", function () {
      shield(false);
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    initPrivacy();
    updateTimes();
    setInterval(updateTimes, 60 * 1000);
  });

  document.addEventListener("alpine:init", function () {
    window.Alpine.data("notes", function (initialCount) {
      return {
        name: "",
        content: "",
        website: "", // honeypot
        submitting: false,
        error: "",
        count: initialCount,

        // Grow the textarea a whole ruled line at a time so text stays on the lines.
        grow: function (el) {
          var line = parseFloat(getComputedStyle(el).lineHeight) || 30;
          el.style.height = "auto";
          el.style.height = Math.max(3, Math.ceil(el.scrollHeight / line)) * line + "px";
        },

        submit: function () {
          if (this.submitting) return;
          this.error = "";
          if (!this.content.trim()) {
            this.error = "Please write a note before saving it.";
            this.$refs.content.focus();
            return;
          }
          var self = this;
          this.submitting = true;
          fetch("/api/comments", {
            method: "POST",
            headers: { "Content-Type": "application/json", Accept: "text/html" },
            body: JSON.stringify({ author_name: this.name, content: this.content, website: this.website }),
          })
            .then(function (res) {
              if (res.status === 201) return res.text();
              return res.json().then(
                function (body) {
                  throw new Error(body.error || "Couldn't save your note. Please try again.");
                },
                function () {
                  throw new Error("Couldn't save your note. Please try again.");
                }
              );
            })
            .then(function (html) {
              var feed = self.$refs.feed;
              feed.insertAdjacentHTML("afterbegin", html);
              updateTimes(feed.firstElementChild);
              self.content = "";
              self.$nextTick(function () {
                self.grow(self.$refs.content);
              });
              self.count++;
              track("story_comment_posted", {});
            })
            .catch(function (err) {
              self.error = err && err.message ? err.message : "Couldn't save your note. Please try again.";
            })
            .finally(function () {
              self.submitting = false;
            });
        },

        // Author only: soft-delete a note, then fade it out of the list.
        remove: function (li) {
          if (!li || !confirm("Delete this note? Readers will no longer see it.")) return;
          var self = this;
          var gone = function () {
            li.style.opacity = "0";
            setTimeout(function () {
              li.remove();
              self.count = Math.max(0, self.count - 1);
            }, 300);
          };
          this.error = "";
          postJSON("/api/admin/delete-comment", { id: li.dataset.commentId }).then(gone, function (err) {
            if (err.status === 404) return gone(); // already deleted elsewhere
            self.error = err.status === 401 ? "Your author session has ended. Sign in again." : err.message;
          });
        },
      };
    });

    // Hidden author tools. Opened by a triple-click on the footer "©"
    // (dispatches admin-open) or Ctrl+Shift+A; Edit Story dispatches story-edit.
    window.Alpine.data("admin", function (isAdmin) {
      return {
        isAdmin: isAdmin,
        openDialog: null, // "login" | "editor" | null
        password: "",
        error: "",
        busy: false,
        dirty: false,

        open: function () {
          this.error = "";
          this.openDialog = "login";
          var self = this;
          this.$nextTick(function () {
            if (self.$refs.password) self.$refs.password.focus();
          });
        },

        openEditor: function () {
          if (!this.isAdmin) return;
          this.error = "";
          this.openDialog = "editor";
          var self = this;
          this.$nextTick(function () {
            self.$refs.body.focus();
          });
        },

        shortcut: function (e) {
          if (e.ctrlKey && e.shiftKey && !e.altKey && (e.key === "A" || e.key === "a")) {
            e.preventDefault();
            this.open();
          }
        },

        close: function () {
          if (this.busy || !this.openDialog) return;
          if (this.openDialog === "editor" && this.dirty) {
            if (!confirm("Discard your unsaved changes to the story?")) return;
            this.$refs.title.value = this.$refs.title.defaultValue;
            this.$refs.body.value = this.$refs.body.defaultValue;
            this.dirty = false;
          }
          this.openDialog = null;
          this.password = "";
          this.error = "";
        },

        login: function () {
          if (this.busy) return;
          if (!this.password) {
            this.error = "Enter the author password.";
            return;
          }
          var self = this;
          this.busy = true;
          this.error = "";
          postJSON("/api/login", { password: this.password })
            .then(function () {
              // Sign-in starts from the footer; land at the top instead, where
              // the Author mode ribbon is. A reload normally restores the old
              // scroll position, so turn that off first.
              if ("scrollRestoration" in history) history.scrollRestoration = "manual";
              window.scrollTo(0, 0);
              location.reload(); // the server renders the author controls
            })
            .catch(function (err) {
              self.error = err.message;
              self.password = "";
              self.busy = false;
            });
        },

        logout: function () {
          postJSON("/api/logout").finally(function () {
            location.reload();
          });
        },

        saveStory: function () {
          if (this.busy) return;
          var self = this;
          this.busy = true;
          this.error = "";
          postJSON("/api/admin/edit-story", { title: this.$refs.title.value, content: this.$refs.body.value })
            .then(function () {
              self.dirty = false;
              location.reload(); // re-render the story from the saved text
            })
            .catch(function (err) {
              self.error = err.status === 401 ? "Your author session has ended. Copy your text, then sign in again." : err.message;
              self.busy = false;
            });
        },
      };
    });

    window.Alpine.data("reader", function () {
      return {
        dark: document.documentElement.classList.contains("dark"),
        progress: 0, // 0..1 share of the story that has reached the bottom of the viewport
        scrolled: false, // currently past the first nudge of scrolling
        sent: {},

        init: function () {
          var self = this;
          var queued = false;
          var schedule = function () {
            if (queued) return;
            queued = true;
            requestAnimationFrame(function () {
              queued = false;
              self.measure();
            });
          };
          window.addEventListener("scroll", schedule, { passive: true });
          window.addEventListener("resize", schedule, { passive: true });
          this.measure();
        },

        measure: function () {
          this.scrolled = window.scrollY > 24;
          var story = this.$refs.story;
          if (!story) return;
          var rect = story.getBoundingClientRect();
          var seen = (window.innerHeight - rect.top) / rect.height;
          this.progress = Math.min(1, Math.max(0, seen));
          // Milestones only count once the reader has actually scrolled,
          // so a tall desktop viewport doesn't fire 25% on load.
          if (!this.scrolled) return;
          var pct = this.progress * 100;
          for (var i = 0; i < MILESTONES.length; i++) {
            var m = MILESTONES[i];
            if (pct >= m - 0.5 && !this.sent[m]) {
              this.sent[m] = true;
              track("story_scroll_milestone", { percent: m });
              if (m === 100) track("story_completed", { active_seconds: activeSeconds });
            }
          }
        },

        toggleTheme: function () {
          this.dark = !this.dark;
          document.documentElement.classList.toggle("dark", this.dark);
          try {
            localStorage.setItem("theme", this.dark ? "dark" : "light");
          } catch (e) {}
        },
      };
    });
  });
})();
