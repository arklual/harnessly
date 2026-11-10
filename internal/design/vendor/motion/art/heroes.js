// Heroes for tk.rig (HRN-652): three bodies, one set of part names, so one script of actions plays on any of them.
//   hero-person   a figure with arms in two parts and legs
//   hero-round    a round mascot with a leaf on top and small arms
//   hero-robot    a boxy robot with a screen face and an antenna
// Parts (data-part): root > body > head > face > eyeL eyeR (pupilL pupilR) browL browR mouth cheekL cheekR; armL armR (foreL foreR);
// legL legR; shadow; tuft (follows the head, optional). Each hero is 400 x 600 and stands on y = 560.
(function () {
  'use strict';
  if (!window.tk || !window.tk.art) throw new Error('heroes.js needs art.js before it');

  // flip: a leaf drawing made on the left, shown on the right (no parts inside it: gsap does not move it).
  function flip(side, svg) {
    return side === 'L' ? svg : '<g transform="matrix(-1 0 0 1 400 0)">' + svg + '</g>';
  }

  var MOUTH_SHAPES = ['smile', 'closed', 'open', 'mid', 'o', 'wide', 'grin', 'sad', 'flat'];

  // Attribute list of a mouth: the path in its resting shape and every shape the rig can morph it to.
  function mouth(shapes, rest, extra) {
    var a = ' data-part="mouth" data-rest="' + rest + '" d="' + shapes[rest] + '"' + (extra || '');
    for (var i = 0; i < MOUTH_SHAPES.length; i++) {
      if (shapes[MOUTH_SHAPES[i]]) a += ' data-m-' + MOUTH_SHAPES[i] + '="' + shapes[MOUTH_SHAPES[i]] + '"';
    }
    return a;
  }

  // The tongue is a second path that morphs with the mouth: it shows inside the open shapes and shrinks to nothing in the shut ones.
  function tongue(shapes, rest, dot) {
    var all = {}, i, k;
    for (i = 0; i < MOUTH_SHAPES.length; i++) {
      k = MOUTH_SHAPES[i];
      all[k] = shapes[k] || dot;
    }
    return mouth(all, rest, '');
  }

  // ---------------------------------------------------------------- person

  var PERSON_MOUTH = {
    smile: 'M178 188 Q200 205 222 188 Q200 205 178 188Z',
    closed: 'M182 192 Q200 197 218 192 Q200 197 182 192Z',
    open: 'M181 186 Q200 195 219 186 Q218 215 200 216 Q182 215 181 186Z',
    mid: 'M184 187 Q200 195 216 187 Q215 204 200 205 Q185 204 184 187Z',
    o: 'M192 190 Q200 187 208 190 Q213 199 208 207 Q200 211 192 207 Q187 199 192 190Z',
    wide: 'M176 186 Q200 193 224 186 Q222 208 200 210 Q178 208 176 186Z',
    grin: 'M173 184 Q200 193 227 184 Q225 211 200 213 Q175 211 173 184Z',
    sad: 'M181 200 Q200 187 219 200 Q200 187 181 200Z',
    flat: 'M183 193 Q200 194 217 193 Q200 194 183 193Z',
  };
  var PERSON_TONGUE = {
    open: 'M189 207 Q200 199 211 207 Q209 214 200 214 Q191 214 189 207Z',
    mid: 'M192 198 Q200 194 208 198 Q207 203 200 203 Q193 203 192 198Z',
    o: 'M196 200 Q200 197 204 200 Q206 204 200 205 Q194 204 196 200Z',
    wide: 'M188 200 Q200 195 212 200 Q211 206 200 207 Q189 206 188 200Z',
    grin: 'M187 202 Q200 196 213 202 Q212 209 200 210 Q188 209 187 202Z',
  };

  function personArm(side) {
    // side 'L' is on the left of the picture. Rest: the arm hangs a little outward.
    var m = side === 'L' ? function (x) { return x; } : function (x) { return 400 - x; };
    var sh = m(153) + ' 258', el = m(134) + ' 328', wr = m(125) + ' 398';
    return '<g data-part="arm' + side + '" data-pivot="' + sh + '" data-tip="' + wr + '">' +
      '<path class="st s-acc" stroke-width="36" d="M' + sh + ' L' + el + '"/>' +
      '<g data-part="fore' + side + '" data-pivot="' + el + '" data-bend="' + (side === 'L' ? 6 : -6) + '">' +
      '<path class="st s-skin" stroke-width="25" d="M' + el + ' L' + wr + '"/>' +
      '<circle class="f-skin" cx="' + m(124) + '" cy="406" r="17"/>' +
      '</g>' +
      '<circle class="f-acc" cx="' + m(153) + '" cy="258" r="18"/>' +
      '</g>';
  }

  function personLeg(side) {
    var m = side === 'L' ? function (x) { return x; } : function (x) { return 400 - x; };
    return '<g data-part="leg' + side + '" data-pivot="' + m(172) + ' 378">' +
      '<path class="st s-pants" stroke-width="42" d="M' + m(172) + ' 384 L' + m(170) + ' 522"/>' +
      '<path class="st s-dark" stroke-width="12" opacity=".14" d="M' + m(185) + ' 410 L' + m(183) + ' 520"/>' +
      flip(side, '<path class="f-white" d="M146 536 q0-13 13-13 h20 q19 0 19 17 v8 q0 10-10 10 h-32 q-10 0-10-10z"/>' +
        '<path class="f-steel" d="M146 552 h52 v2 q0 8-10 8 h-32 q-10 0-10-8z"/>') +
      '</g>';
  }

  window.tk.art.define('hero-person', {
    w: 400, h: 600,
    rig: { kind: 'person', breath: 0.016, up: 150, wag: 22, swing: 26, bend: 6, lean: 3 },
    body:
      '<g data-part="root" data-pivot="200 560">' +
      '<ellipse data-part="shadow" data-pivot="200 562" class="sh" cx="200" cy="562" rx="112" ry="16"/>' +
      personLeg('L') + personLeg('R') +
      '<g data-part="body" data-pivot="200 396">' +
      '<path class="f-pants" d="M148 372 L252 372 L254 408 Q254 420 242 420 L158 420 Q146 420 146 408Z"/>' +
      '<path class="f-skin2" d="M184 196 h32 v56 q-16 10-32 0z"/>' +
      '<path class="f-acc" d="M147 262 Q147 238 172 236 L228 236 Q253 238 253 262 L257 384 Q257 398 243 398 L157 398 Q143 398 143 384Z"/>' +
      '<path class="sh" d="M228 312 L254 312 L257 384 Q257 398 243 398 L220 398Z"/>' +
      '<path class="f-skin" d="M180 237 Q200 272 220 237 Z"/>' +
      '<path class="st s-acc2" stroke-width="5" d="M178 236 Q200 268 222 236"/>' +
      '<rect class="f-acc2" x="143" y="384" width="114" height="14" rx="7" opacity=".5"/>' +
      '<path class="hl" d="M160 262 Q162 246 176 244 L170 262Z"/>' +
      personArm('L') + personArm('R') +
      '<g data-part="head" data-pivot="200 226">' +
      '<circle class="f-skin2" cx="140" cy="158" r="13"/><circle class="f-skin2" cx="260" cy="158" r="13"/>' +
      '<path class="f-skin" d="M139 148 Q139 86 200 86 Q261 86 261 148 Q261 216 200 220 Q139 216 139 148Z"/>' +
      '<path class="sh" d="M232 96 Q261 104 261 148 Q261 216 200 220 Q236 196 232 96Z" opacity=".55"/>' +
      '<g data-part="face" data-pivot="200 160">' +
      '<g data-part="eyeL" data-pivot="172 158"><ellipse class="f-white" cx="172" cy="158" rx="14" ry="16"/>' +
      '<g data-part="pupilL"><circle class="f-dark" cx="172" cy="160" r="8.5"/><circle class="f-white" cx="175" cy="156" r="2.8"/></g></g>' +
      '<g data-part="eyeR" data-pivot="228 158"><ellipse class="f-white" cx="228" cy="158" rx="14" ry="16"/>' +
      '<g data-part="pupilR"><circle class="f-dark" cx="228" cy="160" r="8.5"/><circle class="f-white" cx="231" cy="156" r="2.8"/></g></g>' +
      '<path data-part="browL" data-pivot="172 128" class="st s-hair" stroke-width="6.5" d="M155 131 Q172 120 189 130"/>' +
      '<path data-part="browR" data-pivot="228 128" class="st s-hair" stroke-width="6.5" d="M211 130 Q228 120 245 131"/>' +
      '<path class="st s-skin2" stroke-width="4" d="M200 166 q-6 13 2 16"/>' +
      '<ellipse data-part="cheekL" data-pivot="152 184" class="f-warm" cx="152" cy="184" rx="13" ry="9" opacity="0"/>' +
      '<ellipse data-part="cheekR" data-pivot="248 184" class="f-warm" cx="248" cy="184" rx="13" ry="9" opacity="0"/>' +
      '<path class="f-dark s-dark" stroke-width="5" stroke-linejoin="round"' + mouth(PERSON_MOUTH, 'smile') + '/>' +
      '<path class="f-tongue"' + tongue(PERSON_TONGUE, 'smile', 'M200 197 Q200 197 200 197 Q200 197 200 197Z') + '/>' +
      '</g>' +
      '<path class="f-hair" d="M135 150 C124 86 160 62 204 66 C250 66 280 96 265 150 C259 126 244 108 220 106 C196 122 168 118 150 118 C142 126 137 138 135 150Z"/>' +
      '<path class="f-hair2" d="M168 74 C190 66 226 66 246 82 C226 80 196 82 168 74Z" opacity=".8"/>' +
      '</g>' +
      '</g>' +
      '</g>',
  });

  // ---------------------------------------------------------------- round mascot

  var ROUND_MOUTH = {
    smile: 'M172 372 Q200 398 228 372 Q200 398 172 372Z',
    closed: 'M180 378 Q200 384 220 378 Q200 384 180 378Z',
    open: 'M176 368 Q200 382 224 368 Q222 408 200 410 Q178 408 176 368Z',
    mid: 'M180 370 Q200 381 220 370 Q218 394 200 396 Q182 394 180 370Z',
    o: 'M190 372 Q200 367 210 372 Q216 385 210 398 Q200 403 190 398 Q184 385 190 372Z',
    wide: 'M170 368 Q200 380 230 368 Q227 400 200 402 Q173 400 170 368Z',
    grin: 'M166 364 Q200 378 234 364 Q231 406 200 408 Q169 406 166 364Z',
    sad: 'M176 388 Q200 368 224 388 Q200 368 176 388Z',
    flat: 'M180 380 Q200 382 220 380 Q200 382 180 380Z',
  };
  var ROUND_TONGUE = {
    open: 'M187 397 Q200 387 213 397 Q211 406 200 406 Q189 406 187 397Z',
    mid: 'M190 386 Q200 381 210 386 Q209 393 200 393 Q191 393 190 386Z',
    o: 'M195 390 Q200 387 205 390 Q208 396 200 397 Q192 396 195 390Z',
    wide: 'M186 388 Q200 381 214 388 Q212 397 200 398 Q188 397 186 388Z',
    grin: 'M184 392 Q200 384 216 392 Q214 403 200 404 Q186 403 184 392Z',
  };

  function roundArm(side) {
    var m = side === 'L' ? function (x) { return x; } : function (x) { return 400 - x; };
    var sh = m(86) + ' 392';
    return '<g data-part="arm' + side + '" data-pivot="' + sh + '" data-tip="' + m(40) + ' 452">' +
      '<path class="st s-acc2" stroke-width="40" d="M' + sh + ' L' + m(52) + ' 440"/>' +
      '<circle class="f-white" cx="' + m(44) + '" cy="452" r="21"/>' +
      '<path class="sh" d="M' + m(44) + ' 472 a21 21 0 0 0 ' + (side === 'L' ? '' : '-') + '18 -22 a21 21 0 0 1 ' + (side === 'L' ? '-' : '') + '18 22z" opacity=".6"/>' +
      '</g>';
  }

  function roundFoot(side) {
    var m = side === 'L' ? function (x) { return x; } : function (x) { return 400 - x; };
    return '<g data-part="leg' + side + '" data-pivot="' + m(158) + ' 520">' +
      '<ellipse class="f-acc2" cx="' + m(156) + '" cy="540" rx="42" ry="22"/>' +
      '<path class="hl" d="M' + m(132) + ' 534 q10-10 24-10 q-14 2-24 10z"/>' +
      '</g>';
  }

  window.tk.art.define('hero-round', {
    w: 400, h: 600,
    rig: { kind: 'round', breath: 0.028, up: 128, wag: 20, swing: 18, lean: 4, bob: 8 },
    body:
      '<g data-part="root" data-pivot="200 560">' +
      '<ellipse data-part="shadow" data-pivot="200 564" class="sh" cx="200" cy="564" rx="118" ry="16"/>' +
      roundFoot('L') + roundFoot('R') +
      '<g data-part="body" data-pivot="200 530">' +
      '<ellipse class="f-acc" cx="200" cy="378" rx="140" ry="152"/>' +
      '<path class="sh" d="M268 244 Q342 300 336 400 Q326 500 232 528 Q298 470 292 372 Q290 300 268 244Z" opacity=".9"/>' +
      '<ellipse class="f-soft" cx="200" cy="452" rx="92" ry="76"/>' +
      '<path class="hl" d="M104 322 Q114 276 156 252 Q122 290 118 336Z"/>' +
      roundArm('L') + roundArm('R') +
      '<g data-part="head" data-pivot="200 380">' +
      '<g data-part="tuft" data-pivot="200 232">' +
      '<path class="st s-leaf2" stroke-width="9" d="M200 234 Q196 196 214 168"/>' +
      '<path class="f-leaf" d="M214 170 Q214 130 250 118 Q258 158 214 170Z"/>' +
      '<path class="f-leaf" d="M208 184 Q192 150 156 150 Q156 188 208 184Z"/>' +
      '<path class="sh" d="M214 170 Q234 152 250 118 Q258 158 214 170Z"/>' +
      '</g>' +
      '<g data-part="face" data-pivot="200 340">' +
      '<g data-part="eyeL" data-pivot="152 326"><ellipse class="f-white" cx="152" cy="326" rx="25" ry="30"/>' +
      '<g data-part="pupilL"><circle class="f-dark" cx="154" cy="330" r="15"/><circle class="f-white" cx="159" cy="323" r="5"/></g></g>' +
      '<g data-part="eyeR" data-pivot="248 326"><ellipse class="f-white" cx="248" cy="326" rx="25" ry="30"/>' +
      '<g data-part="pupilR"><circle class="f-dark" cx="250" cy="330" r="15"/><circle class="f-white" cx="255" cy="323" r="5"/></g></g>' +
      '<path data-part="browL" data-pivot="152 284" class="st s-acc2" stroke-width="8" d="M130 288 Q152 274 174 286"/>' +
      '<path data-part="browR" data-pivot="248 284" class="st s-acc2" stroke-width="8" d="M226 286 Q248 274 270 288"/>' +
      '<ellipse data-part="cheekL" data-pivot="118 372" class="f-warm" cx="118" cy="372" rx="18" ry="12" opacity=".0"/>' +
      '<ellipse data-part="cheekR" data-pivot="282 372" class="f-warm" cx="282" cy="372" rx="18" ry="12" opacity=".0"/>' +
      '<path class="f-dark s-dark" stroke-width="6" stroke-linejoin="round"' + mouth(ROUND_MOUTH, 'smile') + '/>' +
      '<path class="f-tongue"' + tongue(ROUND_TONGUE, 'smile', 'M200 385 Q200 385 200 385 Q200 385 200 385Z') + '/>' +
      '</g>' +
      '</g>' +
      '</g>' +
      '</g>',
  });

  // ---------------------------------------------------------------- robot

  var ROBOT_MOUTH = {
    smile: 'M168 168 Q200 184 232 168 Q200 184 168 168Z',
    closed: 'M174 170 L226 170 Q200 172 174 170Z',
    open: 'M170 164 L230 164 Q232 190 200 192 Q168 190 170 164Z',
    mid: 'M174 166 L226 166 Q226 182 200 184 Q174 182 174 166Z',
    o: 'M188 162 Q200 158 212 162 Q218 175 212 188 Q200 192 188 188 Q182 175 188 162Z',
    wide: 'M164 164 L236 164 Q238 186 200 188 Q162 186 164 164Z',
    grin: 'M162 160 L238 160 Q238 192 200 194 Q162 192 162 160Z',
    sad: 'M172 178 Q200 160 228 178 Q200 160 172 178Z',
    flat: 'M172 172 L228 172 Q200 173 172 172Z',
  };

  function robotArm(side) {
    var m = side === 'L' ? function (x) { return x; } : function (x) { return 400 - x; };
    var sh = m(122) + ' 270', el = m(100) + ' 336', wr = m(92) + ' 404';
    return '<g data-part="arm' + side + '" data-pivot="' + sh + '" data-tip="' + wr + '">' +
      '<path class="st s-steel2" stroke-width="26" d="M' + sh + ' L' + el + '"/>' +
      '<g data-part="fore' + side + '" data-pivot="' + el + '" data-bend="' + (side === 'L' ? 5 : -5) + '">' +
      '<path class="st s-steel3" stroke-width="22" d="M' + el + ' L' + wr + '"/>' +
      '<circle class="f-steel" cx="' + m(100) + '" cy="336" r="15"/>' +
      '<rect class="f-acc" x="' + (m(92) - 17) + '" y="388" width="34" height="38" rx="15"/>' +
      '<rect class="f-bg" x="' + (m(92) - 3) + '" y="414" width="6" height="14" rx="3"/>' +
      '</g>' +
      '<circle class="f-steel" cx="' + m(122) + '" cy="270" r="21"/>' +
      '<circle class="f-acc" cx="' + m(122) + '" cy="270" r="8"/>' +
      '</g>';
  }

  function robotLeg(side) {
    var m = side === 'L' ? function (x) { return x; } : function (x) { return 400 - x; };
    return '<g data-part="leg' + side + '" data-pivot="' + m(168) + ' 396">' +
      '<path class="st s-steel3" stroke-width="26" d="M' + m(168) + ' 400 L' + m(168) + ' 516"/>' +
      '<rect class="f-steel2" x="' + (side === 'L' ? 150 : 400 - 186) + '" y="442" width="36" height="14" rx="7"/>' +
      flip(side, '<path class="f-acc2" d="M140 540 q0-18 18-18 h22 q16 0 16 18 v12 q0 6-6 6 h-44 q-6 0-6-6z"/>' +
        '<path class="hl" d="M146 536 q4-10 14-10 h16 q-14 2-30 10z"/>') +
      '</g>';
  }

  window.tk.art.define('hero-robot', {
    w: 400, h: 600,
    rig: { kind: 'robot', breath: 0.012, up: 146, wag: 24, swing: 20, bend: 5, lean: 2.5 },
    body:
      '<g data-part="root" data-pivot="200 560">' +
      '<ellipse data-part="shadow" data-pivot="200 562" class="sh" cx="200" cy="562" rx="96" ry="13"/>' +
      robotLeg('L') + robotLeg('R') +
      '<g data-part="body" data-pivot="200 410">' +
      '<rect class="f-steel3" x="170" y="360" width="60" height="50" rx="12"/>' +
      '<rect class="f-white" x="124" y="244" width="152" height="160" rx="42"/>' +
      '<path class="sh" d="M238 246 Q276 250 276 290 L276 364 Q276 404 236 404 Q252 330 238 246Z"/>' +
      '<rect class="f-soft" x="152" y="278" width="96" height="64" rx="20"/>' +
      '<circle class="f-acc" cx="176" cy="310" r="11"/><circle class="f-good" cx="206" cy="310" r="8"/><circle class="f-warm" cx="230" cy="310" r="6"/>' +
      '<rect class="f-steel" x="150" y="360" width="100" height="10" rx="5"/>' +
      '<path class="hl" d="M142 268 Q142 252 160 250 L158 262Z"/>' +
      robotArm('L') + robotArm('R') +
      '<g data-part="head" data-pivot="200 238">' +
      '<g data-part="tuft" data-pivot="200 76">' +
      '<path class="st s-steel3" stroke-width="8" d="M200 78 L200 40"/>' +
      '<circle class="f-warm" cx="200" cy="32" r="14"/><circle class="hl" cx="195" cy="27" r="5"/>' +
      '</g>' +
      '<rect class="f-steel2" x="104" y="140" width="22" height="46" rx="10"/><rect class="f-steel2" x="274" y="140" width="22" height="46" rx="10"/>' +
      '<rect class="f-steel" x="116" y="74" width="168" height="152" rx="50"/>' +
      '<path class="sh" d="M240 76 Q284 80 284 124 L284 176 Q284 226 234 226 Q256 150 240 76Z" opacity=".8"/>' +
      '<rect class="f-dark" x="136" y="94" width="128" height="112" rx="34"/>' +
      '<g data-part="face" data-pivot="200 150">' +
      '<g data-part="eyeL" data-pivot="170 134"><rect class="f-acc3" x="154" y="116" width="32" height="38" rx="12"/>' +
      '<g data-part="pupilL"><rect class="f-white" x="162" y="124" width="12" height="14" rx="5"/></g></g>' +
      '<g data-part="eyeR" data-pivot="230 134"><rect class="f-acc3" x="214" y="116" width="32" height="38" rx="12"/>' +
      '<g data-part="pupilR"><rect class="f-white" x="222" y="124" width="12" height="14" rx="5"/></g></g>' +
      '<path data-part="browL" data-pivot="170 104" class="st s-acc3" stroke-width="6" d="M154 106 L186 106"/>' +
      '<path data-part="browR" data-pivot="230 104" class="st s-acc3" stroke-width="6" d="M214 106 L246 106"/>' +
      '<ellipse data-part="cheekL" data-pivot="148 170" class="f-warm" cx="148" cy="172" rx="9" ry="6" opacity="0"/>' +
      '<ellipse data-part="cheekR" data-pivot="252 170" class="f-warm" cx="252" cy="172" rx="9" ry="6" opacity="0"/>' +
      '<path class="f-acc3 s-acc3" stroke-width="5" stroke-linejoin="round"' + mouth(ROBOT_MOUTH, 'smile') + '/>' +
      '</g>' +
      '<path class="hl" d="M132 96 Q136 80 160 78 L152 90Z"/>' +
      '</g>' +
      '</g>' +
      '</g>',
  });
})();
