//! Local mode attaches to a runtime that already listens (the installer's launchd or systemd unit).
//! Only when HARNESSLY_RUNTIME_BIN names a binary does the app start its own and stop it on quit.

use crate::config::DEFAULT_LOCAL_PORT;
use std::collections::HashMap;
use std::io::{Read, Write};
use std::net::{Ipv4Addr, SocketAddr, TcpStream};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

pub struct LocalRuntime {
    pub port: u16,
    child: Option<Child>,
}

impl LocalRuntime {
    pub fn stop(&mut self) {
        if let Some(mut child) = self.child.take() {
            let _ = child.kill();
            let _ = child.wait();
        }
    }
}

impl Drop for LocalRuntime {
    fn drop(&mut self) {
        self.stop();
    }
}

pub fn local_port_from_env(env: &HashMap<String, String>) -> u16 {
    env.get("HARNESSLY_DESKTOP_PORT")
        .or_else(|| env.get("PORT"))
        .and_then(|v| v.trim().parse::<u16>().ok())
        .filter(|p| *p > 0)
        .unwrap_or(DEFAULT_LOCAL_PORT)
}

/// True when a Harnessly runtime answers 200 on /api/health at this loopback port.
pub fn is_runtime_healthy(port: u16, timeout: Duration) -> bool {
    let addr = SocketAddr::from((Ipv4Addr::LOCALHOST, port));
    let Ok(mut stream) = TcpStream::connect_timeout(&addr, timeout) else { return false };
    let _ = stream.set_read_timeout(Some(timeout));
    let _ = stream.set_write_timeout(Some(timeout));
    let request = format!("GET /api/health HTTP/1.0\r\nHost: 127.0.0.1:{port}\r\nConnection: close\r\n\r\n");
    if stream.write_all(request.as_bytes()).is_err() {
        return false;
    }
    let mut head = [0u8; 32];
    let mut read = 0;
    while read < 12 {
        match stream.read(&mut head[read..]) {
            Ok(0) | Err(_) => break,
            Ok(n) => read += n,
        }
    }
    is_ok_status_line(&head[..read])
}

fn is_ok_status_line(head: &[u8]) -> bool {
    let text = String::from_utf8_lossy(head);
    let mut parts = text.split_whitespace();
    matches!((parts.next(), parts.next()), (Some(v), Some("200")) if v.starts_with("HTTP/"))
}

pub fn ensure_local_runtime(env: &HashMap<String, String>, wait: Duration) -> Option<LocalRuntime> {
    let port = local_port_from_env(env);
    let probe = Duration::from_millis(1500);
    if is_runtime_healthy(port, probe) {
        return Some(LocalRuntime { port, child: None });
    }
    let bin = env.get("HARNESSLY_RUNTIME_BIN").filter(|b| !b.is_empty())?;
    let child = Command::new(bin)
        .envs(env)
        .env("RUNTIME_GATE_LISTEN", format!("127.0.0.1:{port}"))
        .env("RUNTIME_GATE_UPSTREAM", "")
        .env("RUNTIME_APP_BASE_PATH", "/")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .ok()?;
    let mut runtime = LocalRuntime { port, child: Some(child) };
    let started = Instant::now();
    while started.elapsed() < wait {
        if let Some(child) = runtime.child.as_mut() {
            if child.try_wait().ok().flatten().is_some() {
                runtime.child = None;
                return None;
            }
        }
        if is_runtime_healthy(port, probe) {
            return Some(runtime);
        }
        std::thread::sleep(Duration::from_millis(500));
    }
    runtime.stop();
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;

    fn env(pairs: &[(&str, &str)]) -> HashMap<String, String> {
        pairs.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect()
    }

    #[test]
    fn port_from_env() {
        assert_eq!(local_port_from_env(&env(&[])), DEFAULT_LOCAL_PORT);
        assert_eq!(local_port_from_env(&env(&[("HARNESSLY_DESKTOP_PORT", "9000")])), 9000);
        assert_eq!(local_port_from_env(&env(&[("PORT", "9001")])), 9001);
        assert_eq!(local_port_from_env(&env(&[("HARNESSLY_DESKTOP_PORT", "0")])), DEFAULT_LOCAL_PORT);
        assert_eq!(local_port_from_env(&env(&[("HARNESSLY_DESKTOP_PORT", "70000")])), DEFAULT_LOCAL_PORT);
    }

    #[test]
    fn status_lines() {
        assert!(is_ok_status_line(b"HTTP/1.1 200 OK\r\n"));
        assert!(!is_ok_status_line(b"HTTP/1.1 503 Service Unavailable"));
        assert!(!is_ok_status_line(b"garbage"));
    }

    fn serve_once(status: &'static str) -> u16 {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        std::thread::spawn(move || {
            if let Ok((mut s, _)) = listener.accept() {
                let mut buf = [0u8; 512];
                let _ = s.read(&mut buf);
                let _ = s.write_all(format!("HTTP/1.1 {status}\r\nContent-Length: 0\r\n\r\n").as_bytes());
            }
        });
        port
    }

    #[test]
    fn health_probe() {
        assert!(is_runtime_healthy(serve_once("200 OK"), Duration::from_secs(2)));
        assert!(!is_runtime_healthy(serve_once("503 Service Unavailable"), Duration::from_secs(2)));
        let closed = TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port();
        assert!(!is_runtime_healthy(closed, Duration::from_millis(300)));
    }

    #[test]
    fn attaches_without_binary() {
        let port = serve_once("200 OK");
        let rt = ensure_local_runtime(&env(&[("HARNESSLY_DESKTOP_PORT", &port.to_string())]), Duration::from_secs(1));
        assert_eq!(rt.map(|r| r.port), Some(port));
        let closed = TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port();
        assert!(ensure_local_runtime(&env(&[("HARNESSLY_DESKTOP_PORT", &closed.to_string())]), Duration::from_secs(1))
            .is_none());
    }
}
