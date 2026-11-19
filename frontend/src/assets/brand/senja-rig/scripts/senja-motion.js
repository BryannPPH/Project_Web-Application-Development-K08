// Senja motion library: every animation as keyed poses over the rig.
// Inlined into each HyperFrames composition by scripts/build-rig.mjs (edit here, then rebuild).
//
// SenjaMotion.build(name, prefix, cycles) -> a paused GSAP timeline that plays cycles
// loops of the animation on the rig whose ids are prefixed with "<prefix>-".
// Each cycle starts and ends on the neutral pose (or on the same pose), so it loops seamlessly.
// Re-defined per scene on purpose: each scene must query its own DOM.
window.SenjaMotion = (() => {
  const LENGTH = { idle: 4, wave: 4, thinking: 5, happy: 2.4, walk: 1, tail: 3, stretch: 2.6, sleepy: 6 };

  function rig(prefix) {
    const sel = (id) => "#" + prefix + "-" + id;
    const node = (id) => document.querySelector(sel(id));
    const pivoted = [...document.querySelectorAll("#" + prefix + "-senja-rig [data-pivot]")];
    // Rotation/scale origins come from the rig's data-pivot, in SVG user units.
    pivoted.forEach((n) => gsap.set(n, { svgOrigin: n.dataset.pivot }));
    return { sel, node, pivoted };
  }

  // Keyed poses: frames = [[time, vars, ease?], ...] relative to t0. Each frame's state is
  // reached at its time, tweening from the previous frame's time (0 for the first).
  function keys(tl, r, id, t0, frames) {
    let prev = 0;
    for (const [t, vars, ease] of frames) {
      tl.to(r.sel(id), { ...vars, duration: Math.max(t - prev, 0.001), ease: ease || "sine.inOut" }, t0 + prev);
      prev = t;
    }
  }

  // Face states are drawn alternatives; swap them on a frame like a 2D animator would.
  const EYES = { open: ["eye-l-open", "eye-r-open"], closed: ["eye-l-closed", "eye-r-closed"], happy: ["eye-l-happy", "eye-r-happy"] };
  const MOUTH = { smile: ["mouth-smile"], open: ["mouth-open"], small: ["mouth-small"] };
  function face(tl, r, t, { eyes, mouth, brows } = {}) {
    if (eyes) for (const [k, ids] of Object.entries(EYES)) ids.forEach((id) => tl.set(r.sel(id), { opacity: k === eyes ? 1 : 0 }, t));
    if (mouth) for (const [k, ids] of Object.entries(MOUTH)) ids.forEach((id) => tl.set(r.sel(id), { opacity: k === mouth ? 1 : 0 }, t));
    if (brows !== undefined) tl.to(r.sel("brows"), { opacity: brows ? 1 : 0, duration: 0.15, ease: "power1.out" }, t);
  }

  function reset(tl, r, t) {
    r.pivoted.forEach((n) => tl.set(n, { x: 0, y: 0, rotation: 0, scaleX: 1, scaleY: 1, skewX: 0, svgOrigin: n.dataset.pivot }, t));
    tl.set([r.sel("eye-l-open"), r.sel("eye-r-open")], { x: 0, y: 0, scaleY: 1 }, t);
    tl.set(r.sel("paw-l-pad"), { opacity: 0 }, t);
    tl.set(r.sel("brows"), { opacity: 0 }, t);
    face(tl, r, t, { eyes: "open", mouth: "smile" });
  }

  // Blink: quick close, slower open, closed-lid drawing at the bottom; right eye trails 2 frames.
  function blink(tl, r, t) {
    [["l", 0], ["r", 0.066]].forEach(([s, d]) => {
      const open = r.sel("eye-" + s + "-open"), shut = r.sel("eye-" + s + "-closed");
      const o = r.node("eye-" + s).dataset.pivot;
      tl.to(open, { scaleY: 0.08, svgOrigin: o, duration: 0.07, ease: "power2.in" }, t + d);
      tl.set(open, { opacity: 0 }, t + d + 0.06);
      tl.set(shut, { opacity: 1 }, t + d + 0.06);
      tl.set(open, { opacity: 1 }, t + d + 0.1);
      tl.set(shut, { opacity: 0 }, t + d + 0.1);
      tl.to(open, { scaleY: 1, svgOrigin: o, duration: 0.12, ease: "power2.out" }, t + d + 0.1);
    });
  }

  // Tail: rotation from the base plus a lagging shear, so the tip trails like a soft brush.
  function tailSway(tl, r, t, L, amp) {
    keys(tl, r, "tail", t, [[L / 2, { rotation: amp }], [L, { rotation: 0 }]]);
    keys(tl, r, "tail", t, [[L * 0.12, { skewX: 0 }], [L * 0.62, { skewX: -amp * 0.6 }], [L, { skewX: 0 }]]);
  }

  // Coffee steam (pagi outfit): two wisps rise and fade, half a period apart. Each loop ends
  // where it began (a wisp jumps back down only while invisible).
  function steam(tl, r, t, L) {
    const P = L / 2;
    keys(tl, r, "steam-a", t, [
      [0.001, { y: 0, opacity: 0 }], [P / 2, { y: -5, opacity: 0.85 }], [P, { y: -10, opacity: 0 }],
      [P + 0.001, { y: 0 }], [1.5 * P, { y: -5, opacity: 0.85 }], [L, { y: -10, opacity: 0 }],
    ]);
    keys(tl, r, "steam-b", t, [
      [0.001, { y: -5, opacity: 0.85 }], [P / 2, { y: -10, opacity: 0 }], [P / 2 + 0.001, { y: 0 }],
      [P, { y: -5, opacity: 0.85 }], [1.5 * P, { y: -10, opacity: 0 }], [1.5 * P + 0.001, { y: 0 }],
      [L, { y: -5, opacity: 0.85 }],
    ]);
  }

  // Shared breathing: chest swells from the hips, head and scarf ride it a beat later.
  function breathe(tl, r, t, L, amt = 1) {
    const h = L / 2;
    keys(tl, r, "body", t, [[h, { scaleY: 1 + 0.025 * amt, scaleX: 1 + 0.012 * amt }], [L, { scaleY: 1, scaleX: 1 }]]);
    keys(tl, r, "head", t, [[0.12, { y: 0 }], [h + 0.12, { y: -2.2 * amt }], [L, { y: 0 }]]);
    keys(tl, r, "scarf", t, [[0.08, { y: 0 }], [h + 0.08, { y: -1.6 * amt }], [L, { y: 0 }]]);
  }

  const ANIM = {
    idle(tl, r, t) {
      const L = LENGTH.idle, h = L / 2;
      keys(tl, r, "senja", t, [[h, { y: -5 }], [L, { y: 0 }]]);
      breathe(tl, r, t, L);
      keys(tl, r, "head", t, [[0.12, { rotation: 0 }], [h + 0.12, { rotation: -1.2 }], [L, { rotation: 0 }]]);
      keys(tl, r, "scarf-ends", t, [[0.25, { rotation: 0 }], [h + 0.25, { rotation: 4 }], [L, { rotation: 0 }]]);
      keys(tl, r, "arm-l", t, [[0.05, { rotation: 0 }], [h + 0.05, { rotation: 2.5 }], [L, { rotation: 0 }]]);
      keys(tl, r, "arm-r", t, [[0.15, { rotation: 0 }], [h + 0.15, { rotation: -1.6 }], [L, { rotation: 0 }]]);
      keys(tl, r, "prop-lantern", t, [[0.3, { rotation: 0 }], [h + 0.3, { rotation: 3.5 }], [L, { rotation: 0 }]]);
      tailSway(tl, r, t, L, 6);
      steam(tl, r, t, L);
      blink(tl, r, t + 1.5);
      keys(tl, r, "ear-l", t, [[2.9, { rotation: 0 }], [3.0, { rotation: -7 }, "power2.out"], [3.35, { rotation: 0 }, "back.out(2)"]]);
    },

    // Raise the free paw (palm out), wave from the shoulder with the wrist lagging, lower, settle.
    wave(tl, r, t) {
      const L = LENGTH.wave;
      keys(tl, r, "arm-l", t, [
        [0.15, { rotation: -8 }, "power2.out"], // anticipation: tuck in
        [0.55, { rotation: 100 }, "back.out(1.4)"],
        [0.83, { rotation: 88 }], [1.11, { rotation: 112 }], [1.39, { rotation: 88 }],
        [1.67, { rotation: 112 }], [1.95, { rotation: 88 }], [2.23, { rotation: 104 }],
        [2.55, { rotation: 100 }],
        [3.05, { rotation: -4 }, "power2.inOut"],
        [3.35, { rotation: 0 }, "power1.out"],
      ]);
      keys(tl, r, "paw-l", t, [
        [0.55, { rotation: 0 }], [0.97, { rotation: 16 }], [1.25, { rotation: -16 }], [1.53, { rotation: 16 }],
        [1.81, { rotation: -16 }], [2.09, { rotation: 14 }], [2.4, { rotation: 0 }],
      ]);
      tl.set(r.sel("paw-l-pad"), { opacity: 1 }, t + 0.42);
      tl.set(r.sel("paw-l-pad"), { opacity: 0 }, t + 2.85);
      keys(tl, r, "senja", t, [[0.55, { rotation: -1.5 }, "power2.out"], [2.6, { rotation: -1.5 }], [3.3, { rotation: 0 }]]);
      keys(tl, r, "head", t, [[0.7, { rotation: -4 }, "power2.out"], [2.6, { rotation: -4 }], [3.35, { rotation: 0 }]]);
      keys(tl, r, "ear-l", t, [[0.5, { rotation: -5 }, "power2.out"], [2.8, { rotation: -5 }], [3.2, { rotation: 0 }, "back.out(2)"]]);
      keys(tl, r, "scarf-ends", t, [[0.6, { rotation: 6 }], [1.4, { rotation: -2 }], [2.2, { rotation: 5 }], [3.4, { rotation: 0 }]]);
      face(tl, r, t + 0.5, { eyes: "happy", mouth: "open" });
      face(tl, r, t + 2.9, { eyes: "open", mouth: "smile" });
      breathe(tl, r, t, L, 0.6);
      steam(tl, r, t, L);
      keys(tl, r, "tail", t, [[1, { rotation: 7 }], [2, { rotation: -2 }], [3, { rotation: 7 }], [L, { rotation: 0 }]]);
    },

    // Head tilts, brows lift, eyes search up-right, then up-left, then settle; paw taps the belly.
    thinking(tl, r, t) {
      const L = LENGTH.thinking;
      keys(tl, r, "head", t, [[0.55, { rotation: 7 }, "power2.inOut"], [4.2, { rotation: 7 }], [4.8, { rotation: 0 }, "power2.inOut"]]);
      keys(tl, r, "ear-r", t, [[0.7, { rotation: 7 }, "power2.out"], [4.1, { rotation: 7 }], [4.7, { rotation: 0 }]]);
      keys(tl, r, "ear-l", t, [[0.8, { rotation: -4 }], [4.1, { rotation: -4 }], [4.75, { rotation: 0 }]]);
      face(tl, r, t + 0.4, { mouth: "small", brows: true });
      face(tl, r, t + 4.4, { mouth: "smile", brows: false });
      keys(tl, r, "brows", t, [[0.6, { y: -2 }], [4.3, { y: -2 }], [4.7, { y: 0 }]]);
      [["eye-l-open", 0], ["eye-r-open", 0.05]].forEach(([id, d]) =>
        keys(tl, r, id, t + d, [
          [0.65, { x: 3.5, y: -4 }, "power3.out"], [1.9, { x: 3.5, y: -4 }],
          [2.15, { x: -3.5, y: -4 }, "power3.out"], [3.3, { x: -3.5, y: -4 }],
          [3.55, { x: 1.5, y: -2 }, "power3.out"], [4.3, { x: 1.5, y: -2 }],
          [4.6, { x: 0, y: 0 }, "power2.out"],
        ]),
      );
      blink(tl, r, t + 2.6);
      keys(tl, r, "arm-l", t, [[0.5, { rotation: -14 }, "power2.out"], [4.2, { rotation: -14 }], [4.75, { rotation: 0 }]]);
      keys(tl, r, "paw-l", t, [
        [1.0, { rotation: 0 }], [1.12, { rotation: -14 }, "power2.out"], [1.28, { rotation: 0 }, "power2.in"],
        [1.42, { rotation: -14 }, "power2.out"], [1.58, { rotation: 0 }, "power2.in"],
        [3.5, { rotation: 0 }], [3.62, { rotation: -14 }, "power2.out"], [3.78, { rotation: 0 }, "power2.in"],
      ]);
      breathe(tl, r, t, L, 0.7);
      tailSway(tl, r, t, L, 4);
    },

    // Anticipation squash, stretch on take-off, arms up mid-air, land squash, settle; ears, tail,
    // scarf and lantern follow through after the body.
    happy(tl, r, t) {
      keys(tl, r, "senja", t, [
        [0.25, { scaleY: 0.92, scaleX: 1.05 }, "power2.out"],
        [0.42, { scaleY: 1.06, scaleX: 0.96 }, "power2.in"],
        [0.75, { scaleY: 1, scaleX: 1 }, "power1.out"],
        [1.0, { scaleY: 1.04, scaleX: 0.98 }, "power2.in"],
        [1.1, { scaleY: 0.93, scaleX: 1.05 }, "power2.out"],
        [1.45, { scaleY: 1, scaleX: 1 }, "back.out(2)"],
      ]);
      keys(tl, r, "senja", t, [[0.3, { y: 0 }], [0.75, { y: -55 }, "power2.out"], [1.04, { y: 0 }, "power2.in"]]);
      face(tl, r, t + 0.2, { eyes: "happy", mouth: "open" });
      face(tl, r, t + 2.1, { eyes: "open", mouth: "smile" });
      keys(tl, r, "arm-l", t, [[0.25, { rotation: -10 }, "power2.out"], [0.5, { rotation: 72 }, "power2.out"], [1.0, { rotation: 60 }], [1.4, { rotation: 0 }, "power2.inOut"]]);
      keys(tl, r, "arm-r", t, [[0.28, { rotation: 8 }, "power2.out"], [0.54, { rotation: -35 }, "power2.out"], [1.02, { rotation: -28 }], [1.42, { rotation: 0 }, "power2.inOut"]]);
      // The lantern counter-rotates against the arm so it keeps hanging, plus a lagging swing.
      keys(tl, r, "prop-lantern", t, [[0.28, { rotation: -8 }, "power2.out"], [0.6, { rotation: 45 }], [1.08, { rotation: 16 }], [1.45, { rotation: 6 }], [1.85, { rotation: -3 }], [2.3, { rotation: 0 }]]);
      keys(tl, r, "head", t, [[0.25, { rotation: 2 }], [0.6, { rotation: -4 }], [1.12, { rotation: 3 }], [1.5, { rotation: 0 }]]);
      keys(tl, r, "ear-l", t, [[0.5, { rotation: -9 }], [1.04, { rotation: 7 }], [1.4, { rotation: 0 }, "back.out(2)"]]);
      keys(tl, r, "ear-r", t, [[0.54, { rotation: 9 }], [1.08, { rotation: -7 }], [1.44, { rotation: 0 }, "back.out(2)"]]);
      keys(tl, r, "tail", t, [[0.5, { rotation: -12 }], [1.05, { rotation: 14 }], [1.5, { rotation: -4 }], [2.0, { rotation: 0 }]]);
      keys(tl, r, "scarf-ends", t, [[0.6, { rotation: -22 }], [1.05, { rotation: 16 }], [1.5, { rotation: -5 }], [2.0, { rotation: 0 }]]);
      keys(tl, r, "leg-l", t, [[0.45, { rotation: 0, y: 0 }], [0.6, { rotation: 10, y: -5 }], [0.95, { rotation: 0, y: 0 }]]);
      keys(tl, r, "leg-r", t, [[0.48, { rotation: 0, y: 0 }], [0.63, { rotation: -10, y: -5 }], [0.97, { rotation: 0, y: 0 }]]);
    },

    // In-place walk, 1 s = two steps. Legs alternate lift/plant/push; arms counter-swing;
    // body bobs twice; head, lantern, scarf and tail lag behind.
    walk(tl, r, t) {
      keys(tl, r, "leg-l", t, [[0.25, { y: -9, rotation: -8 }, "sine.out"], [0.5, { y: 0, rotation: 0 }, "sine.in"], [0.75, { rotation: 5 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "leg-r", t, [[0.25, { rotation: 5 }], [0.5, { rotation: 0 }], [0.75, { y: -9, rotation: -8 }, "sine.out"], [1.0, { y: 0, rotation: 0 }, "sine.in"]]);
      keys(tl, r, "arm-l", t, [[0.25, { rotation: 10 }], [0.5, { rotation: 0 }], [0.75, { rotation: -10 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "arm-r", t, [[0.25, { rotation: -8 }], [0.5, { rotation: 0 }], [0.75, { rotation: 8 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "senja", t, [[0.25, { y: -5 }], [0.5, { y: 0 }], [0.75, { y: -5 }], [1.0, { y: 0 }]]);
      keys(tl, r, "body", t, [[0.25, { rotation: -1.5 }], [0.5, { rotation: 0 }], [0.75, { rotation: 1.5 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "head", t, [[0.07, { y: 0, rotation: 0 }], [0.32, { y: -2, rotation: 1.2 }], [0.57, { y: 0, rotation: 0 }], [0.82, { y: -2, rotation: -1.2 }], [1.0, { y: 0, rotation: 0 }]]);
      keys(tl, r, "prop-lantern", t, [[0.25, { rotation: 6 }], [0.75, { rotation: -6 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "scarf-ends", t, [[0.3, { rotation: 6 }], [0.8, { rotation: -3 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "ear-l", t, [[0.3, { rotation: -3 }], [0.55, { rotation: 0 }], [0.8, { rotation: -3 }], [1.0, { rotation: 0 }]]);
      keys(tl, r, "tail", t, [[0.5, { rotation: 6 }], [1.0, { rotation: 0 }]]);
    },

    // Tail on its own: a slow, gentle sway (the same tailSway every other loop uses).
    tail(tl, r, t) {
      tailSway(tl, r, t, LENGTH.tail, 9);
    },

    // Morning wake-up (plays once): squash, arms stretch out wide, head back, eyes shut and a
    // big yawn; then relax, eyes open on a blink, settle.
    stretch(tl, r, t) {
      keys(tl, r, "senja", t, [[0.3, { scaleY: 0.97, scaleX: 1.02 }, "power2.out"], [0.9, { scaleY: 1.03, scaleX: 0.99 }, "power2.out"], [1.7, { scaleY: 1.03, scaleX: 0.99 }], [2.2, { scaleY: 1, scaleX: 1 }, "power2.inOut"]]);
      keys(tl, r, "body", t, [[0.35, { scaleY: 1 }], [1.0, { scaleY: 1.05, scaleX: 0.98 }, "power2.out"], [1.65, { scaleY: 1.05, scaleX: 0.98 }], [2.2, { scaleY: 1, scaleX: 1 }, "power2.inOut"]]);
      keys(tl, r, "arm-l", t, [[0.3, { rotation: -6 }, "power2.out"], [1.0, { rotation: 112 }, "power2.out"], [1.6, { rotation: 120 }], [2.25, { rotation: -3 }, "power2.inOut"], [2.6, { rotation: 0 }]]);
      keys(tl, r, "arm-r", t, [[0.33, { rotation: 6 }, "power2.out"], [1.05, { rotation: -112 }, "power2.out"], [1.62, { rotation: -120 }], [2.3, { rotation: 3 }, "power2.inOut"], [2.6, { rotation: 0 }]]);
      keys(tl, r, "head", t, [[0.35, { rotation: 0, y: 0 }], [1.0, { rotation: -6, y: -5 }, "power2.out"], [1.65, { rotation: -7, y: -5 }], [2.25, { rotation: 0, y: 0 }, "power2.inOut"]]);
      keys(tl, r, "ear-l", t, [[0.5, { rotation: 0 }], [1.0, { rotation: -9 }, "power2.out"], [1.7, { rotation: -9 }], [2.3, { rotation: 0 }, "back.out(2)"]]);
      keys(tl, r, "ear-r", t, [[0.55, { rotation: 0 }], [1.05, { rotation: 9 }, "power2.out"], [1.72, { rotation: 9 }], [2.35, { rotation: 0 }, "back.out(2)"]]);
      face(tl, r, t + 0.4, { eyes: "closed", mouth: "open" });
      face(tl, r, t + 1.8, { mouth: "smile" });
      face(tl, r, t + 2.15, { eyes: "open" });
      keys(tl, r, "tail", t, [[0.9, { rotation: 8 }], [1.8, { rotation: -3 }], [2.6, { rotation: 0 }]]);
      keys(tl, r, "scarf-ends", t, [[1.0, { rotation: -6 }], [2.0, { rotation: 4 }], [2.6, { rotation: 0 }]]);
      steam(tl, r, t, LENGTH.stretch);
    },

    // Night: dozing. Eyes shut, slow deep breaths, head droops lower and lower, a little
    // startle (eyes pop open, ears perk), then the eyes slide shut and the head drifts back.
    sleepy(tl, r, t) {
      const L = LENGTH.sleepy;
      face(tl, r, t, { eyes: "closed" });
      // Deep, slow breaths (two per loop); the head is left free for the nod.
      keys(tl, r, "body", t, [[1.5, { scaleY: 1.035, scaleX: 1.018 }], [3, { scaleY: 1, scaleX: 1 }], [4.5, { scaleY: 1.035, scaleX: 1.018 }], [L, { scaleY: 1, scaleX: 1 }]]);
      keys(tl, r, "scarf", t, [[0.08, { y: 0 }], [1.58, { y: -2.4 }], [3.08, { y: 0 }], [4.58, { y: -2.4 }], [L, { y: 0 }]]);
      keys(tl, r, "head", t, [[3.6, { rotation: 6, y: 3 }, "sine.in"], [3.85, { rotation: -1.5, y: -1 }, "power3.out"], [L, { rotation: 0, y: 0 }, "sine.inOut"]]);
      keys(tl, r, "ear-l", t, [[3.6, { rotation: -7 }, "sine.in"], [3.85, { rotation: 2 }, "back.out(2)"], [L, { rotation: 0 }]]);
      keys(tl, r, "ear-r", t, [[3.62, { rotation: 6 }, "sine.in"], [3.88, { rotation: -2 }, "back.out(2)"], [L, { rotation: 0 }]]);
      face(tl, r, t + 3.8, { eyes: "open" });
      face(tl, r, t + 4.6, { eyes: "closed" });
      keys(tl, r, "senja", t, [[3.6, { rotation: 1.2 }, "sine.inOut"], [3.9, { rotation: -0.4 }, "power2.out"], [L, { rotation: 0 }]]);
      keys(tl, r, "prop-lantern", t, [[3.9, { rotation: 0 }], [4.3, { rotation: -5 }, "power2.out"], [5.1, { rotation: 3 }], [L, { rotation: 0 }]]);
      // Malam outfit: the pillow rises with each breath; the nightcap pompom swings on the startle.
      keys(tl, r, "prop-pillow", t, [[1.5, { scaleY: 1.025, scaleX: 1.01 }], [3, { scaleY: 1, scaleX: 1 }], [4.5, { scaleY: 1.025, scaleX: 1.01 }], [L, { scaleY: 1, scaleX: 1 }]]);
      keys(tl, r, "nightcap-pompom", t, [[3.6, { rotation: -6 }, "sine.in"], [3.95, { rotation: 16 }, "power2.out"], [4.55, { rotation: -8 }], [5.25, { rotation: 3 }], [L, { rotation: 0 }]]);
      tailSway(tl, r, t, L, 3);
    },
  };

  function build(name, prefix, cycles = 1) {
    const r = rig(prefix);
    const tl = gsap.timeline({ paused: true });
    reset(tl, r, 0);
    for (let c = 0; c < cycles; c++) ANIM[name](tl, r, c * LENGTH[name]);
    // Pin the scene's length to whole cycles so the loop point is exact.
    tl.set({}, {}, cycles * LENGTH[name]);
    return tl;
  }

  return { build, LENGTH, ANIM };
})();
