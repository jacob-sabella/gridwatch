# NPM (Nginx Proxy Manager) reverse-proxy setup

NPM is the path of least resistance for most homelab reverse proxy setups, but **SSE needs a small tweak** or the timeline will feel frozen.

## Add a proxy host

1. **Hosts → Proxy Hosts → Add Proxy Host**
2. **Details tab:**
   - Domain Names: `gridwatch.your.domain`
   - Scheme: `http`
   - Forward Hostname / IP: the LAN IP of the machine running gridwatch
   - Forward Port: `8080`
   - ✅ **Block Common Exploits**
   - ✅ **Websockets Support** (SSE uses the same upgrade path)
3. **SSL tab:**
   - Pick an existing wildcard cert OR request a new one via Let's Encrypt
   - ✅ **Force SSL**
   - HTTP/2 Support is optional
4. **Advanced tab** — paste this to disable SSE buffering:
   ```nginx
   proxy_buffering off;
   proxy_cache off;
   proxy_set_header X-Accel-Buffering no;

   location /events {
     proxy_buffering off;
     proxy_cache off;
     proxy_read_timeout 3600s;
     proxy_send_timeout 3600s;
     proxy_set_header X-Accel-Buffering no;
   }
   ```
5. **Save**.

## Verify

```bash
curl -sN https://gridwatch.your.domain/events | head -5
```

You should see:
```
: connected

event: revision
data: 123
```
…arriving immediately, not after a 30 s delay. If it hangs, the Advanced-tab snippet didn't apply — double-check the location block.

## Common gotchas

- **`proxy_read_timeout` too low** — default is 60 s; SSE connections idle longer between revisions. The snippet above raises it to 1 hour for `/events` specifically.
- **CloudFlare in front of NPM** — CloudFlare buffers SSE unless the host is Orange-clouded OFF or you're on a plan with Cache Rules / Response Buffering toggled off. Easiest fix: set the DNS record to DNS-only (grey cloud) for `gridwatch.your.domain`.
- **`http://` forward with `https://` external** — forward scheme stays `http://` even when the outside is SSL. That's NPM terminating TLS for you; gridwatch doesn't need its own cert.
