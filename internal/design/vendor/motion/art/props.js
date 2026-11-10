// Things for an explainer (HRN-652), drawn flat and rounded in the deck's colours. Parts a scene can animate are named:
//   prop-laptop (screen)   prop-phone (screen)   prop-chart (bar, line, dot, arrow)   prop-cloud (arrow)   prop-server (led)
//   prop-gear (gear)   prop-rocket (flame)   prop-bulb (glow, ray)   prop-doc (row)   prop-folder (sheet)   prop-bubble (dot)
//   prop-check (ring, check)
// Pivots are in data-pivot ("x y" in the drawing's own units); tk.rig.pivots(svg) sets them for gsap.
(function () {
  'use strict';
  if (!window.tk || !window.tk.art) throw new Error('props.js needs art.js before it');
  var define = window.tk.art.define;
  var FIT = 'xMidYMid meet';

  // smooth: a curve through points (Catmull-Rom as cubic Béziers).
  function smooth(p) {
    var d = 'M' + p[0][0] + ' ' + p[0][1];
    for (var i = 0; i < p.length - 1; i++) {
      var a = p[i - 1] || p[i], b = p[i], c = p[i + 1], e = p[i + 2] || c;
      d += ' C' + (b[0] + (c[0] - a[0]) / 6).toFixed(1) + ' ' + (b[1] + (c[1] - a[1]) / 6).toFixed(1) + ' ' +
        (c[0] - (e[0] - b[0]) / 6).toFixed(1) + ' ' + (c[1] - (e[1] - b[1]) / 6).toFixed(1) + ' ' + c[0] + ' ' + c[1];
    }
    return d;
  }

  // ---------------------------------------------------------------- laptop
  define('prop-laptop', {
    w: 360, h: 260, fit: FIT,
    body:
      '<ellipse class="sh" cx="180" cy="240" rx="150" ry="10"/>' +
      '<rect class="f-steel2" x="52" y="22" width="256" height="172" rx="18"/>' +
      '<rect class="f-dark" x="60" y="30" width="240" height="156" rx="11"/>' +
      '<g data-part="screen">' +
      '<rect class="f-white" x="66" y="36" width="228" height="144" rx="7"/>' +
      '<rect class="f-acc" x="66" y="36" width="228" height="22" rx="7"/><rect class="f-acc" x="66" y="48" width="228" height="10"/>' +
      '<circle class="f-on" cx="80" cy="47" r="3.5" opacity=".9"/><circle class="f-on" cx="92" cy="47" r="3.5" opacity=".6"/>' +
      '<rect class="f-panel" x="66" y="58" width="52" height="122"/>' +
      '<rect class="f-line" x="76" y="72" width="32" height="7" rx="3.5"/><rect class="f-line" x="76" y="88" width="26" height="7" rx="3.5"/><rect class="f-line" x="76" y="104" width="30" height="7" rx="3.5"/>' +
      '<rect class="f-soft" x="130" y="70" width="70" height="46" rx="8"/><rect class="f-soft" x="210" y="70" width="70" height="46" rx="8"/>' +
      '<rect class="f-acc" x="138" y="96" width="14" height="12" rx="3"/><rect class="f-acc3" x="156" y="86" width="14" height="22" rx="3"/><rect class="f-acc" x="174" y="78" width="14" height="30" rx="3"/>' +
      '<circle class="f-warm" cx="245" cy="94" r="14"/><path class="f-sun" d="M245 94 L245 80 A14 14 0 0 1 258 99Z"/>' +
      '<rect class="f-line" x="130" y="128" width="150" height="8" rx="4"/><rect class="f-line" x="130" y="144" width="110" height="8" rx="4"/><rect class="f-line" x="130" y="160" width="130" height="8" rx="4"/>' +
      '</g>' +
      '<path class="f-steel" d="M26 194 H334 L346 222 Q348 236 332 236 H28 Q12 236 14 222Z"/>' +
      '<path class="sh" d="M26 194 H334 L346 222 Q348 236 332 236 H300 Q320 214 312 194Z" opacity=".7"/>' +
      '<rect class="f-steel2" x="148" y="198" width="64" height="9" rx="4.5"/>' +
      '<path class="hl" d="M32 200 H100 L96 204 H30Z"/>',
  });

  // ---------------------------------------------------------------- phone
  define('prop-phone', {
    w: 176, h: 320, fit: FIT,
    body:
      '<ellipse class="sh" cx="88" cy="308" rx="62" ry="8"/>' +
      '<rect class="f-dark" x="14" y="6" width="148" height="298" rx="34"/>' +
      '<rect class="f-dark2" x="14" y="6" width="148" height="298" rx="34" opacity=".5"/>' +
      '<g data-part="screen">' +
      '<rect class="f-white" x="24" y="16" width="128" height="278" rx="26"/>' +
      '<rect class="f-acc" x="24" y="16" width="128" height="78" rx="26"/><rect class="f-acc" x="24" y="60" width="128" height="34"/>' +
      '<circle class="f-on" cx="52" cy="68" r="14" opacity=".92"/><rect class="f-on" x="74" y="58" width="56" height="8" rx="4" opacity=".92"/><rect class="f-on" x="74" y="72" width="38" height="7" rx="3.5" opacity=".6"/>' +
      '<rect class="f-soft" x="36" y="108" width="74" height="30" rx="14"/><rect class="f-acc" x="66" y="148" width="74" height="30" rx="14"/>' +
      '<rect class="f-soft" x="36" y="188" width="88" height="30" rx="14"/><rect class="f-acc3" x="82" y="228" width="58" height="26" rx="13"/>' +
      '<rect class="f-panel" x="34" y="262" width="108" height="24" rx="12"/><circle class="f-acc" cx="126" cy="274" r="8"/>' +
      '</g>' +
      '<rect class="f-dark" x="60" y="22" width="56" height="15" rx="7.5"/>' +
      '<rect class="hl" x="20" y="14" width="6" height="150" rx="3" opacity=".3"/>',
  });

  // ---------------------------------------------------------------- chart
  var bars = [[62, 56], [118, 82], [174, 70], [230, 112], [286, 146]];
  var barSvg = '';
  bars.forEach(function (b, i) {
    var top = 238 - b[1];
    barSvg += '<rect data-part="bar" data-pivot="' + (b[0] + 17) + ' 238" class="' + (i === bars.length - 1 ? 'f-acc' : 'f-acc3') + '" x="' + b[0] + '" y="' + top + '" width="34" height="' + b[1] + '" rx="9"/>';
  });
  var pts = [[79, 184], [135, 160], [191, 168], [247, 116], [303, 70]];
  define('prop-chart', {
    w: 380, h: 290, fit: FIT,
    body:
      '<rect class="sh" x="14" y="22" width="352" height="262" rx="26"/>' +
      '<rect class="f-white" x="10" y="12" width="352" height="262" rx="26"/>' +
      '<rect class="f-panel" x="10" y="12" width="352" height="52" rx="26"/><rect class="f-panel" x="10" y="40" width="352" height="24"/>' +
      '<rect class="f-acc" x="32" y="30" width="86" height="12" rx="6"/><rect class="f-line" x="130" y="30" width="46" height="12" rx="6"/>' +
      '<path class="st s-line" stroke-width="2.5" d="M34 118 H340 M34 158 H340 M34 198 H340"/>' +
      barSvg +
      '<path class="st s-line" stroke-width="4" d="M34 238 H340"/>' +
      '<path data-part="line" class="st s-warm" stroke-width="8" d="' + smooth(pts) + '"/>' +
      pts.map(function (p) { return '<circle data-part="dot" data-pivot="' + p[0] + ' ' + p[1] + '" class="f-white s-warm" stroke-width="5" cx="' + p[0] + '" cy="' + p[1] + '" r="8.5"/>'; }).join('') +
      '<g transform="rotate(-38 303 70)"><path data-part="arrow" data-pivot="312 70" class="f-warm" d="M300 54 L326 70 L300 86 Z" opacity="0"/></g>',
  });

  // ---------------------------------------------------------------- cloud
  define('prop-cloud', {
    w: 340, h: 220, fit: FIT,
    body:
      '<ellipse class="sh" cx="170" cy="208" rx="120" ry="9"/>' +
      '<g class="f-sky"><circle cx="102" cy="132" r="56"/><circle cx="170" cy="96" r="70"/><circle cx="244" cy="128" r="54"/><rect x="60" y="130" width="230" height="60" rx="30"/></g>' +
      '<path class="f-sky2" d="M60 160 Q70 190 100 190 H262 Q292 188 292 158 Q290 176 268 178 H92 Q68 178 60 160Z" opacity=".75"/>' +
      '<path class="hl" d="M120 72 Q146 44 180 46 Q150 52 134 78Z"/>' +
      '<g data-part="arrow" data-pivot="170 128"><path class="st s-white" stroke-width="14" d="M170 154 V108 M146 128 L170 104 L194 128"/></g>',
  });

  // ---------------------------------------------------------------- server
  function unit(y, leds) {
    var s = '<rect class="f-dark2" x="22" y="' + y + '" width="176" height="62" rx="14"/>' +
      '<rect class="f-steel3" x="22" y="' + y + '" width="176" height="6" rx="3" opacity=".5"/>' +
      '<path class="st s-dark" stroke-width="5" d="M104 ' + (y + 22) + ' H176 M104 ' + (y + 34) + ' H176 M104 ' + (y + 46) + ' H176"/>';
    leds.forEach(function (c, i) { s += '<circle data-part="led" data-pivot="' + (46 + i * 20) + ' ' + (y + 31) + '" class="f-' + c + '" cx="' + (46 + i * 20) + '" cy="' + (y + 31) + '" r="6"/>'; });
    return s;
  }
  define('prop-server', {
    w: 220, h: 290, fit: FIT,
    body:
      '<ellipse class="sh" cx="110" cy="280" rx="92" ry="9"/>' +
      '<rect class="f-dark" x="10" y="8" width="200" height="262" rx="22"/>' +
      '<rect class="f-steel3" x="26" y="262" width="26" height="16" rx="6"/><rect class="f-steel3" x="168" y="262" width="26" height="16" rx="6"/>' +
      unit(20, ['good', 'good', 'warm']) + unit(92, ['good', 'acc3', 'good']) + unit(164, ['good', 'good', 'good']) +
      '<path class="hl" d="M16 24 H22 V250 H16Z" opacity=".25"/>',
  });

  // ---------------------------------------------------------------- gear
  function gear(cx, cy, ro, ri, n) {
    var d = '', step = Math.PI * 2 / n, a;
    for (var i = 0; i < n; i++) {
      a = i * step;
      var p = [[-0.30, ri], [-0.17, ro], [0.17, ro], [0.30, ri]];
      for (var j = 0; j < 4; j++) {
        var ang = a + p[j][0] * step * 1.6 + step / 2;
        d += (d ? ' L' : 'M') + (cx + Math.cos(ang) * p[j][1]).toFixed(1) + ' ' + (cy + Math.sin(ang) * p[j][1]).toFixed(1);
      }
    }
    return d + 'Z';
  }
  define('prop-gear', {
    w: 260, h: 260, fit: FIT,
    body:
      '<g data-part="gear" data-pivot="130 130">' +
      '<path class="f-acc s-acc" stroke-width="10" stroke-linejoin="round" d="' + gear(130, 130, 118, 92, 10) + '"/>' +
      '<circle class="f-acc2" cx="130" cy="130" r="84"/>' +
      '<circle class="f-acc" cx="130" cy="130" r="72"/>' +
      '<path class="hl" d="M72 108 A62 62 0 0 1 124 66 A70 70 0 0 0 72 108Z"/>' +
      '<circle class="f-bg" cx="130" cy="130" r="34"/><circle class="f-soft" cx="130" cy="130" r="24"/>' +
      '</g>',
  });

  // ---------------------------------------------------------------- rocket
  define('prop-rocket', {
    w: 180, h: 350, fit: FIT,
    body:
      '<g data-part="flame" data-pivot="90 250">' +
      '<path class="f-warm" d="M60 250 Q56 296 90 336 Q124 296 120 250Z"/><path class="f-sun" d="M74 250 Q72 282 90 308 Q108 282 106 250Z"/></g>' +
      '<path class="f-acc2" d="M44 168 L10 240 Q8 256 26 250 L52 232Z"/><path class="f-acc2" d="M136 168 L170 240 Q172 256 154 250 L128 232Z"/>' +
      '<path class="f-white" d="M90 14 C146 56 156 140 148 232 L32 232 C24 140 34 56 90 14Z"/>' +
      '<path class="sh" d="M112 36 C150 72 154 150 148 232 L116 232 C126 150 126 80 112 36Z" opacity=".9"/>' +
      '<path class="f-acc" d="M90 14 C112 30 126 54 134 84 L46 84 C54 54 68 30 90 14Z"/>' +
      '<circle class="f-steel" cx="90" cy="142" r="34"/><circle class="f-sky" cx="90" cy="142" r="26"/><path class="hl" d="M72 134 Q78 120 92 118 Q80 126 76 142Z"/>' +
      '<rect class="f-acc" x="32" y="204" width="116" height="16" rx="8"/>' +
      '<rect class="f-steel2" x="62" y="230" width="56" height="24" rx="8"/>',
  });

  // ---------------------------------------------------------------- bulb
  var rays = '';
  for (var r = 0; r < 7; r++) {
    var ang = (-90 + (r - 3) * 28) * Math.PI / 180;
    rays += '<path data-part="ray" data-pivot="' + (90 + Math.cos(ang) * 112).toFixed(1) + ' ' + (98 + Math.sin(ang) * 112).toFixed(1) + '" class="st s-sun2" stroke-width="9" d="M' +
      (90 + Math.cos(ang) * 112).toFixed(1) + ' ' + (98 + Math.sin(ang) * 112).toFixed(1) + ' L' + (90 + Math.cos(ang) * 140).toFixed(1) + ' ' + (98 + Math.sin(ang) * 140).toFixed(1) + '"/>';
  }
  define('prop-bulb', {
    w: 180, h: 300, fit: FIT,
    body:
      rays +
      '<circle data-part="glow" data-pivot="90 98" class="f-sun" cx="90" cy="98" r="100" opacity=".16"/>' +
      '<path class="f-sun" d="M90 28 C136 28 166 62 164 104 C162 138 138 152 132 184 L48 184 C42 152 18 138 16 104 C14 62 44 28 90 28Z"/>' +
      '<path class="sh" d="M120 36 C150 52 168 84 164 112 C160 140 140 152 132 184 L112 184 C124 140 142 120 134 78Z" opacity=".4"/>' +
      '<path class="hl" d="M44 66 Q56 44 80 38 Q58 52 54 82Z"/>' +
      '<path class="st s-warm2" stroke-width="5" d="M72 184 L74 130 Q90 110 106 130 L108 184"/>' +
      '<rect class="f-steel" x="46" y="184" width="88" height="16" rx="8"/><rect class="f-steel2" x="50" y="204" width="80" height="16" rx="8"/>' +
      '<rect class="f-steel" x="54" y="224" width="72" height="16" rx="8"/><path class="f-steel3" d="M70 242 H110 Q106 262 90 262 Q74 262 70 242Z"/>',
  });

  // ---------------------------------------------------------------- doc
  define('prop-doc', {
    w: 220, h: 280, fit: FIT,
    body:
      '<rect class="sh" x="30" y="22" width="172" height="248" rx="20"/>' +
      '<path class="f-white" d="M24 34 Q24 14 44 14 H132 L192 74 V250 Q192 266 176 266 H40 Q24 266 24 250Z"/>' +
      '<path class="f-soft" d="M132 14 L192 74 H150 Q132 74 132 56Z"/><path class="sh" d="M132 14 L192 74 H150 Q132 74 132 56Z" opacity=".4"/>' +
      '<rect class="f-acc" x="46" y="40" width="64" height="12" rx="6"/>' +
      '<rect data-part="row" data-pivot="46 94" class="f-line" x="46" y="90" width="124" height="10" rx="5"/>' +
      '<rect data-part="row" data-pivot="46 114" class="f-line" x="46" y="110" width="104" height="10" rx="5"/>' +
      '<rect data-part="row" data-pivot="46 134" class="f-line" x="46" y="130" width="116" height="10" rx="5"/>' +
      '<rect class="f-soft" x="46" y="158" width="124" height="56" rx="12"/>' +
      '<circle class="f-acc" cx="68" cy="186" r="12"/><rect class="f-acc3" x="88" y="176" width="64" height="9" rx="4.5"/><rect class="f-line" x="88" y="192" width="46" height="8" rx="4"/>' +
      '<rect data-part="row" data-pivot="46 236" class="f-line" x="46" y="232" width="82" height="10" rx="5"/>',
  });

  // ---------------------------------------------------------------- folder
  define('prop-folder', {
    w: 280, h: 220, fit: FIT,
    body:
      '<ellipse class="sh" cx="140" cy="208" rx="116" ry="9"/>' +
      '<path class="f-acc2" d="M22 40 Q22 22 40 22 H102 Q112 22 118 32 L128 46 H240 Q258 46 258 64 V180 Q258 198 240 198 H40 Q22 198 22 180Z"/>' +
      '<g data-part="sheet" data-pivot="130 150"><rect class="f-white" x="42" y="54" width="196" height="120" rx="12"/>' +
      '<rect class="f-line" x="62" y="76" width="100" height="10" rx="5"/><rect class="f-line" x="62" y="98" width="140" height="10" rx="5"/></g>' +
      '<g data-part="sheet" data-pivot="140 150"><rect class="f-soft" x="54" y="66" width="196" height="110" rx="12"/>' +
      '<rect class="f-acc3" x="74" y="88" width="80" height="10" rx="5"/></g>' +
      '<path class="f-acc" d="M14 84 Q14 70 30 70 H250 Q266 70 266 86 V180 Q266 200 246 200 H34 Q14 200 14 180Z"/>' +
      '<path class="hl" d="M24 82 H180 Q186 82 180 88 H26Z" opacity=".6"/>' +
      '<path class="sh" d="M14 170 H266 V180 Q266 200 246 200 H34 Q14 200 14 180Z" opacity=".6"/>',
  });

  // ---------------------------------------------------------------- bubble
  define('prop-bubble', {
    w: 300, h: 240, fit: FIT,
    body:
      '<ellipse class="sh" cx="150" cy="226" rx="82" ry="7" opacity=".8"/>' +
      '<path class="f-acc" d="M60 14 H240 Q288 14 288 62 V120 Q288 168 240 168 H128 L70 214 L84 168 H60 Q12 168 12 120 V62 Q12 14 60 14Z"/>' +
      '<path class="sh" d="M240 14 Q288 14 288 62 V120 Q288 168 240 168 H128 L70 214 L84 168 H150 Q250 160 240 14Z" opacity=".5"/>' +
      '<path class="hl" d="M44 40 Q54 28 80 28 H130 Q70 32 52 62Z"/>' +
      '<circle data-part="dot" data-pivot="95 92" class="f-on" cx="95" cy="92" r="15"/><circle data-part="dot" data-pivot="150 92" class="f-on" cx="150" cy="92" r="15"/><circle data-part="dot" data-pivot="205 92" class="f-on" cx="205" cy="92" r="15"/>',
  });

  // ---------------------------------------------------------------- check
  define('prop-check', {
    w: 220, h: 220, fit: FIT,
    body:
      '<circle data-part="ring" data-pivot="110 110" class="f-good" cx="110" cy="110" r="96"/>' +
      '<path class="sh" d="M110 14 A96 96 0 0 1 110 206 A96 96 0 0 0 110 14Z" opacity=".7"/>' +
      '<circle class="f-white" cx="110" cy="110" r="76" opacity=".16"/>' +
      '<path data-part="check" class="st s-white" stroke-width="20" d="M66 112 L98 144 L156 82"/>',
  });
})();
