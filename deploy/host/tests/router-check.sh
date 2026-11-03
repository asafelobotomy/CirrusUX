#!/bin/bash
# The router check against a fake TR-064 router (Python's http.server) that
# wants basic auth and answers from the MAC address in the request.
set -u
H=$(cd "$(dirname "$0")/../.." && pwd)
T=$(mktemp -d); trap 'kill $SRV 2>/dev/null; rm -rf "$T"' EXIT
fail() { echo "FAIL: $*"; exit 1; }
cat > $T/router.py <<'PY'
import base64, http.server, os, sys
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        body = self.rfile.read(int(self.headers['Content-Length'])).decode()
        path = self.path
        # Asked as an HTTP proxy: the request names the router in full.
        if path.startswith('http://'):
            open(os.environ['T'] + '/proxied', 'a').write(path + '\n')
            path = '/' + path.split('/', 3)[3]
        if self.headers.get('Authorization') != 'Basic ' + base64.b64encode(('admin:' + os.environ['PASS']).encode()).decode():
            self.send_response(401); self.send_header('WWW-Authenticate', 'Basic realm="router"'); self.end_headers(); return
        action = self.headers.get('SoapAction', '')
        mac = body.split('<NewMACAddress>')[1].split('<')[0]
        if path == '/upnp/control/hosts' and action.endswith('#X_AVM-DE_WakeOnLANByMACAddress'):
            open(os.environ['T'] + '/woken', 'a').write(mac + '\n')
            self.send_response(200); self.end_headers()
            self.wfile.write(b'<u:X_AVM-DE_WakeOnLANByMACAddressResponse xmlns:u="urn:dslforum-org:service:Hosts:1"></u:X_AVM-DE_WakeOnLANByMACAddressResponse>')
            return
        if path != '/upnp/control/hosts' or 'GetSpecificHostEntry' not in action:
            self.send_response(500); self.end_headers(); return
        active = {'52:54:00:12:34:56': '1', '52:54:00:00:00:01': '0'}
        self.send_response(200 if mac in active else 500); self.end_headers()
        self.wfile.write(('<NewActive>%s</NewActive>' % active[mac] if mac in active else '<errorCode>714</errorCode><errorDescription>NoSuchEntryInArray</errorDescription>').encode())
# Port 0: the kernel picks a free one, written where the test reads it.
srv = http.server.HTTPServer(('127.0.0.1', 0), H)
open(sys.argv[1] + '.tmp', 'w').write(str(srv.server_port)); os.rename(sys.argv[1] + '.tmp', sys.argv[1])
srv.serve_forever()
PY
# A password made now, so nothing else on the machine can contain it.
# A quote and a backslash in it too: curl's config needs them escaped.
PASS="pw\"q\\ $(head -c 8 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export PASS T
python3 -I $T/router.py $T/port & SRV=$!
# Ready once it has a port: no fixed wait, which a busy machine can outrun.
for _ in $(seq 100); do [ -s $T/port ] && break; sleep 0.1; done
PORT=$(cat $T/port 2>/dev/null) || fail "the stand-in router didn't start"
conf() { printf 'ROUTER_URL=http://127.0.0.1:%s\nROUTER_USER=admin\nROUTER_PASSWORD=\047%s\047\nSERVER_MAC=%s\n' $PORT "$PASS" "$1" > $T/router.env; chmod 600 $T/router.env; }
run() { CIRRUS_ROUTER_ENV=$T/router.env $H/gateway/cirrusux-router-check; }

conf 52:54:00:12:34:56; [ "$(run)" = online ] || fail "online"
conf 52:54:00:00:00:01; [ "$(run)" = offline ] || fail "offline"
conf 52:54:00:99:99:99; [ "$(run)" = offline ] || fail "unknown host"
echo "1 online, offline and unknown hosts read right"

conf 52:54:00:12:34:56; chmod 640 $T/router.env
run >/dev/null 2>&1 && fail "a readable secrets file was accepted"
chmod 600 $T/router.env
sed -i "s/^ROUTER_PASSWORD=.*/ROUTER_PASSWORD=wrong/" $T/router.env
run >/dev/null 2>&1 && fail "a wrong password gave an answer"
echo "2 refuses a readable secrets file; a wrong password gives no answer"

conf "52:54:00:12:34:56</NewMACAddress><x>"
run >/dev/null 2>&1 && fail "a MAC with markup was accepted"
CIRRUS_ROUTER_ENV=$T/router.env $H/gateway/cirrusux-router-check extra >/dev/null 2>&1 && fail "arguments accepted"
echo "3 refuses odd MACs and arguments"

# The password must never be on a command line.
conf 52:54:00:12:34:56
( while :; do ps -eo args >> $T/ps; sleep 0.01; done ) & PS=$!
for _ in 1 2 3 4 5; do run >/dev/null; done
kill $PS
grep -qF "${PASS##* }" $T/ps && fail "the password showed in the process list"
echo "4 the password never shows in the process list"

# The wake, under its own name.
wake() { CIRRUS_ROUTER_ENV=$T/router.env $H/gateway/cirrusux-router-wake; }
conf 52:54:00:12:34:56
[ "$(wake)" = "wake sent" ] && grep -qx 52:54:00:12:34:56 $T/woken || fail "wake"
sed -i "s/^ROUTER_PASSWORD=.*/ROUTER_PASSWORD=wrong/" $T/router.env
wake >/dev/null 2>&1 && fail "a wake without the password reported sent"
echo "5 cirrusux-router-wake asks the router to wake the server"

# Through a proxy: the stand-in router is an HTTP proxy too.
conf 52:54:00:12:34:56
sed -i "s|^ROUTER_URL=.*|ROUTER_URL=http://router.invalid:49000|" $T/router.env
echo "ROUTER_PROXY=http://127.0.0.1:$PORT" >> $T/router.env
[ "$(run)" = online ] && [ "$(wake)" = "wake sent" ] || fail "through the proxy"
grep -qx 'http://router.invalid:49000/upnp/control/hosts' $T/proxied || fail "the proxy wasn't used"
sed -i "s|^ROUTER_PROXY=.*|ROUTER_PROXY=ftp://127.0.0.1:1|" $T/router.env
run >/dev/null 2>&1 && fail "a proxy that isn't SOCKS5 or HTTP accepted"
echo "6 ROUTER_PROXY: both calls go through it; other schemes refused"
