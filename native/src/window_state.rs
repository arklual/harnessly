//! Where a new window opens: the saved place when it is still on a screen, else centred on the primary one.

use crate::config::WindowState;

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Rect {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

const MIN_OVERLAP: f64 = 50.0;

pub fn is_visible_on_any_display(bounds: Rect, work_areas: &[Rect]) -> bool {
    work_areas.iter().any(|a| {
        let overlap_x = ((bounds.x + bounds.width).min(a.x + a.width) - bounds.x.max(a.x)).max(0.0);
        let overlap_y = ((bounds.y + bounds.height).min(a.y + a.height) - bounds.y.max(a.y)).max(0.0);
        overlap_x >= MIN_OVERLAP && overlap_y >= MIN_OVERLAP
    })
}

/// Saved position when it is still on a screen, else None (the OS centres the window).
pub fn restore_position(saved: &WindowState, work_areas: &[Rect]) -> Option<(f64, f64)> {
    let (x, y) = (saved.x?, saved.y?);
    let bounds = Rect { x, y, width: saved.width, height: saved.height };
    is_visible_on_any_display(bounds, work_areas).then_some((x, y))
}

/// The size, kept within the screen and above the window's minimum.
pub fn clamp_size(saved: &WindowState, min: (f64, f64), largest: Option<Rect>) -> (f64, f64) {
    let (mut w, mut h) = (saved.width.max(min.0), saved.height.max(min.1));
    if let Some(area) = largest {
        w = w.min(area.width.max(min.0));
        h = h.min(area.height.max(min.1));
    }
    (w, h)
}

/// Each extra window is shifted so it does not cover the previous one exactly.
pub fn cascade_offset(open_windows: usize) -> f64 {
    if open_windows == 0 {
        0.0
    } else {
        ((open_windows * 32) % 200) as f64
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const SCREEN: Rect = Rect { x: 0.0, y: 0.0, width: 1440.0, height: 900.0 };

    fn state(x: Option<f64>, y: Option<f64>) -> WindowState {
        WindowState { x, y, width: 1000.0, height: 700.0, is_maximized: false, is_full_screen: false }
    }

    #[test]
    fn keeps_visible_position() {
        assert_eq!(restore_position(&state(Some(100.0), Some(50.0)), &[SCREEN]), Some((100.0, 50.0)));
    }

    #[test]
    fn drops_position_of_unplugged_screen() {
        assert_eq!(restore_position(&state(Some(3000.0), Some(50.0)), &[SCREEN]), None);
        assert_eq!(restore_position(&state(Some(1420.0), Some(50.0)), &[SCREEN]), None);
        assert_eq!(restore_position(&state(None, None), &[SCREEN]), None);
    }

    #[test]
    fn clamps_size() {
        let big = WindowState { width: 4000.0, height: 3000.0, ..state(None, None) };
        assert_eq!(clamp_size(&big, (800.0, 500.0), Some(SCREEN)), (1440.0, 900.0));
        let tiny = WindowState { width: 100.0, height: 100.0, ..state(None, None) };
        assert_eq!(clamp_size(&tiny, (800.0, 500.0), None), (800.0, 500.0));
    }

    #[test]
    fn cascades() {
        assert_eq!(cascade_offset(0), 0.0);
        assert_eq!(cascade_offset(1), 32.0);
        assert_eq!(cascade_offset(7), 24.0);
    }
}
