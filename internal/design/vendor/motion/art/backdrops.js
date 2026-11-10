// Backdrops for a video's scenes (HRN-652): 1600 x 900, drawn to fill the frame (`slice`, so 9:16 shows the middle). Each has layers
// <g data-layer="far|mid|near" data-depth="0.2|0.5|1"> for tk.parallax; heroes stand on the floor line, y = 640 of 900 from the top.
//   bg-office     a room: wall, window, shelf, plant, frames, floor
//   bg-city       a street: sky, far and near buildings, trees, lamps, road
//   bg-abstract   soft shapes on the theme's colours, for a scene that is about an idea
(function () {
  'use strict';
  if (!window.tk || !window.tk.art) throw new Error('backdrops.js needs art.js before it');
  var define = window.tk.art.define;
  var FIT = 'xMidYMid slice';

  function layer(name, depth, body) {
    return '<g data-layer="' + name + '" data-depth="' + depth + '">' + body + '</g>';
  }

  // ---------------------------------------------------------------- office
  var books = '';
  [['acc', 34, 70], ['warm', 26, 56], ['sun', 30, 64], ['leaf', 24, 52], ['acc3', 32, 72], ['sky2', 28, 60], ['acc2', 26, 66]].reduce(function (x, b) {
    books += '<rect class="f-' + b[0] + '" x="' + x + '" y="' + (440 - b[2]) + '" width="' + b[1] + '" height="' + b[2] + '" rx="5"/>';
    return x + b[1] + 3;
  }, 126);

  define('bg-office', {
    w: 1600, h: 900, fit: FIT,
    body:
      layer('far', 0.2,
        '<rect class="f-soft" x="-200" y="-100" width="2000" height="800"/>' +
        '<g class="f-white" opacity=".55"><rect x="-200" y="-100" width="300" height="800"/><rect x="420" y="-100" width="300" height="800"/><rect x="1040" y="-100" width="300" height="800"/><rect x="1660" y="-100" width="300" height="800"/></g>' +
        '<rect class="f-white" x="690" y="90" width="500" height="380" rx="26"/>' +
        '<rect class="f-sky" x="716" y="116" width="448" height="328" rx="14"/>' +
        '<path class="f-skyp" d="M716 360 Q780 300 850 330 Q900 290 960 320 Q1040 280 1164 350 V430 Q1164 444 1150 444 H730 Q716 444 716 430Z"/>' +
        '<g class="f-white"><circle cx="800" cy="200" r="30"/><circle cx="838" cy="190" r="38"/><circle cx="884" cy="204" r="28"/><rect x="780" y="198" width="124" height="34" rx="17"/></g>' +
        '<g class="f-white" opacity=".85"><circle cx="1050" cy="250" r="22"/><circle cx="1080" cy="242" r="28"/><rect x="1030" y="248" width="94" height="26" rx="13"/></g>' +
        '<rect class="f-white" x="935" y="116" width="14" height="328"/><rect class="f-white" x="716" y="270" width="448" height="14"/>' +
        '<path class="f-white" opacity=".28" d="M716 444 L1164 444 L1380 700 L560 700Z"/>' +
        '<rect class="f-white" x="676" y="468" width="528" height="26" rx="13"/>' +
        '<g transform="translate(1340 150)"><circle class="f-white" r="62"/><circle class="f-line" r="52"/><circle class="f-white" r="46"/>' +
        '<path class="st s-dark" stroke-width="7" d="M0 0 V-30 M0 0 L22 12"/><circle class="f-acc" r="7"/></g>') +
      layer('mid', 0.5,
        '<rect class="f-wood2" x="100" y="150" width="380" height="14" rx="7"/>' +
        '<rect class="f-wood" x="100" y="150" width="380" height="9" rx="4.5"/>' +
        '<rect class="f-wood2" x="100" y="450" width="380" height="14" rx="7"/><rect class="f-wood" x="100" y="450" width="380" height="9" rx="4.5"/>' +
        books + '<rect class="f-white" x="360" y="400" width="86" height="40" rx="8"/><rect class="f-acc3" x="372" y="412" width="62" height="10" rx="5"/>' +
        '<g transform="translate(150 150)"><rect class="f-white" y="-120" width="120" height="120" rx="12"/><rect class="f-accp" x="12" y="-108" width="96" height="96" rx="8"/><circle class="f-warm" cx="44" cy="-70" r="16"/><path class="f-leaf" d="M12 -12 L52 -52 L74 -30 L90 -46 L108 -12Z"/></g>' +
        '<rect class="f-white" x="1250" y="300" width="230" height="150" rx="14"/><rect class="f-accp" x="1264" y="314" width="202" height="122" rx="8"/>' +
        '<path class="st s-acc" stroke-width="8" d="M1284 410 L1340 366 L1386 392 L1446 336"/>' +
        '<g transform="translate(1440 640)"><path class="f-warm2" d="M-60 -120 h120 l-14 120 h-92z"/><path class="f-warm" d="M-60 -120 h120 v16 h-120z"/>' +
        '<g class="f-leaf"><path d="M0 -120 Q-100 -200 -92 -330 Q-10 -300 0 -120Z"/><path d="M0 -120 Q100 -190 96 -320 Q10 -290 0 -120Z"/><path d="M0 -120 Q-20 -250 6 -400 Q40 -260 0 -120Z"/></g>' +
        '<path class="f-leaf2" d="M0 -120 Q10 -250 6 -400 Q40 -260 0 -120Z" opacity=".5"/></g>') +
      layer('near', 1,
        '<rect class="f-line" x="-200" y="610" width="2000" height="34"/>' +
        '<rect class="f-panel" x="-200" y="644" width="2000" height="400"/>' +
        '<path class="f-accp" d="M-200 644 H1800 V672 H-200Z" opacity=".7"/>' +
        '<ellipse class="f-acc3" cx="800" cy="800" rx="330" ry="40" opacity=".25"/>' +
        '<path class="st s-line" stroke-width="3" d="M-200 760 H1800 M-200 860 H1800"/>' +
        '<path class="sh" d="M-200 610 H1800 V620 H-200Z" opacity=".4"/>'),
  });

  // ---------------------------------------------------------------- city
  function houses(list, base, cls, winCls) {
    var s = '';
    list.forEach(function (h) {
      s += '<rect class="' + cls + '" x="' + h[0] + '" y="' + (base - h[2]) + '" width="' + h[1] + '" height="' + h[2] + '" rx="10"/>';
      if (winCls) {
        for (var y = base - h[2] + 36; y < base - 40; y += 58) {
          for (var x = h[0] + 22; x < h[0] + h[1] - 34; x += 46) {
            s += '<rect class="' + winCls + '" x="' + x + '" y="' + y + '" width="26" height="32" rx="6"/>';
          }
        }
      }
    });
    return s;
  }
  var lamps = '';
  [220, 760, 1300].forEach(function (x) {
    lamps += '<path class="st s-steel3" stroke-width="10" d="M' + x + ' 650 V470 Q' + x + ' 440 ' + (x + 40) + ' 440"/>' +
      '<rect class="f-sun" x="' + (x + 28) + '" y="436" width="40" height="16" rx="8"/><rect class="f-steel3" x="' + (x - 18) + '" y="640" width="36" height="14" rx="6"/>';
  });
  var trees = '';
  [[480, 1], [1060, 0.9], [1500, 1.05]].forEach(function (t) {
    trees += '<g transform="translate(' + t[0] + ' 650) scale(' + t[1] + ')"><rect class="f-wood2" x="-9" y="-120" width="18" height="122" rx="8"/>' +
      '<circle class="f-leaf2" cx="-34" cy="-160" r="52"/><circle class="f-leaf" cx="30" cy="-176" r="62"/><circle class="f-leaf" cx="-6" cy="-212" r="52"/><circle class="f-leaf2" cx="44" cy="-130" r="38" opacity=".7"/></g>';
  });
  define('bg-city', {
    w: 1600, h: 900, fit: FIT,
    body:
      layer('far', 0.15,
        '<rect class="f-skyp" x="-200" y="-100" width="2000" height="800"/>' +
        '<circle class="f-sun" cx="1260" cy="190" r="86" opacity=".9"/><circle class="f-white" cx="1260" cy="190" r="130" opacity=".25"/>' +
        '<g class="f-white"><circle cx="300" cy="150" r="34"/><circle cx="346" cy="136" r="46"/><circle cx="400" cy="152" r="32"/><rect x="270" y="150" width="160" height="34" rx="17"/></g>' +
        '<g class="f-white" opacity=".8"><circle cx="820" cy="260" r="24"/><circle cx="854" cy="250" r="32"/><rect x="800" y="260" width="106" height="26" rx="13"/></g>' +
        houses([[-40, 150, 300], [130, 120, 400], [270, 170, 340], [470, 130, 440], [620, 160, 320], [810, 140, 380], [970, 180, 460], [1170, 130, 350], [1320, 170, 420], [1510, 140, 330]], 660, 'f-steelp')) +
      layer('mid', 0.45,
        houses([[40, 210, 360], [290, 190, 480], [510, 230, 330], [770, 200, 520], [1000, 250, 380], [1280, 200, 470], [1500, 180, 340]], 660, 'f-accp', 'f-white') +
        '<g class="sh" opacity=".5">' + houses([[40, 210, 360], [510, 230, 330], [1000, 250, 380]], 660, 'sh') + '</g>') +
      layer('near', 1,
        trees + lamps +
        '<rect class="f-steel" x="-200" y="650" width="2000" height="40"/>' +
        '<rect class="f-steel2" x="-200" y="690" width="2000" height="14"/>' +
        '<rect class="f-dark2" x="-200" y="704" width="2000" height="400"/>' +
        '<g class="f-white" opacity=".85"><rect x="40" y="800" width="170" height="14" rx="7"/><rect x="340" y="800" width="170" height="14" rx="7"/><rect x="640" y="800" width="170" height="14" rx="7"/><rect x="940" y="800" width="170" height="14" rx="7"/><rect x="1240" y="800" width="170" height="14" rx="7"/><rect x="1540" y="800" width="170" height="14" rx="7"/></g>'),
  });

  // ---------------------------------------------------------------- abstract
  define('bg-abstract', {
    w: 1600, h: 900, fit: FIT,
    body:
      layer('far', 0.15,
        '<rect class="f-bg" x="-200" y="-100" width="2000" height="1100"/>' +
        '<circle class="f-soft" cx="260" cy="140" r="420"/>' +
        '<circle class="f-accp" cx="1420" cy="760" r="470" opacity=".7"/>' +
        '<path class="f-soft" d="M900 -60 Q1180 40 1260 260 Q1340 480 1100 520 Q880 540 800 340 Q740 160 900 -60Z" opacity=".9"/>') +
      layer('mid', 0.5,
        '<circle class="f-acc3" cx="1240" cy="170" r="74" opacity=".8"/>' +
        '<rect class="f-warm" x="150" y="520" width="120" height="120" rx="34" transform="rotate(18 210 580)" opacity=".85"/>' +
        '<path class="f-sun" d="M520 140 L600 280 L440 280Z" opacity=".9" stroke="var(--a-sun)" stroke-width="22" stroke-linejoin="round"/>' +
        '<circle class="f-sky" cx="1010" cy="560" r="46" opacity=".9"/><circle class="f-leaf" cx="650" cy="520" r="26"/>' +
        '<g class="f-acc" opacity=".35"><circle cx="1400" cy="300" r="7"/><circle cx="1430" cy="300" r="7"/><circle cx="1460" cy="300" r="7"/><circle cx="1400" cy="330" r="7"/><circle cx="1430" cy="330" r="7"/><circle cx="1460" cy="330" r="7"/></g>' +
        '<path class="st s-acc3" stroke-width="12" d="M330 330 q30 -40 60 0 t60 0 t60 0" opacity=".9"/>') +
      layer('near', 1,
        '<rect class="f-panel" x="-200" y="640" width="2000" height="400"/>' +
        '<ellipse class="f-line" cx="800" cy="790" rx="620" ry="62" opacity=".55"/>' +
        '<rect class="f-line" x="-200" y="636" width="2000" height="8" opacity=".6"/>'),
  });
})();
