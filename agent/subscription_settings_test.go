package agent

import (
	"os/exec"
	"testing"
)

func TestSubscriptionSettings(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("Python unavailable")
	}
	cmd := exec.Command(python, "-c", `
import base64, contextlib, io, json, pathlib, runpy, sys, tempfile, zlib

module = runpy.run_path("common/files/vpnkit-gen.py")["subscription_settings"].__globals__
with tempfile.TemporaryDirectory() as tmp:
    root = pathlib.Path(tmp)
    out = root / "u"
    out.mkdir()
    module["USERS"] = str(root / "users.json")
    xray = {"TUNNEL_NAME": "vpn.example", "XH_PATH": "/xh", "XHTTP_PORT": "443", "ENC_PATH": "/pq", "ENC_PORT": "444"}
    configs = {"sub": {"OUTDIR": str(out), "SUB_NAME": "vpn.example"}, "xray": xray}
    module["env"] = configs.get
    writer = module["write_if_changed"]
    module["write_if_changed"] = lambda path, data, mode, group: writer(path, data, mode)

    def encode(data):
        return base64.urlsafe_b64encode(data).decode().rstrip("=")

    def decode(data):
        return base64.urlsafe_b64decode(data + "=" * (-len(data) % 4))

    def run(*args, data=None):
        sys.argv[1:] = args
        sys.stdin = io.StringIO(json.dumps(data))
        with contextlib.redirect_stdout(io.StringIO()) as result:
            module["main"]()
        return json.loads(result.getvalue())

    conf = "[Interface]\nPrivateKey = original-key\nMTU = 1260\nDNS = 10.9.2.1\n\n[Peer]\nEndpoint = vpn.example:51820\nPersistentKeepalive = 25\n"
    inner = json.dumps({"config": conf, "mtu": "1260", "persistent_keep_alive": "25"})
    raw = json.dumps({"dns1": "10.9.2.1", "dns2": "10.9.2.1", "containers": [
        {"container": "amnezia-awg2", "awg": {"last_config": inner}}]}).encode()
    link = "vpn://" + encode(len(raw).to_bytes(4, "big") + zlib.compress(raw))
    original = {"uuid": "existing-id", "token": "original-basic-token", "hy2": "existing-password",
                "amnezia": {"token": "original-vpn-token", "feed_token": "original-feed-token", "link": link}}
    registry = {"schema": 2, "clients": {"Kisik-Mobile": original}, "vlessenc": {"encryption": "fixture"}}
    module["save_users"](registry)
    module["subs"](registry, xray, None, configs["sub"])
    previous_card = run("show", "Kisik-Mobile")
    original_feed = (out / "original-feed-token").read_bytes()
    original_basic = (out / "original-basic-token").read_bytes()
    assert run("list", "--summary")["settings"]
    assert run("settings-show", "Kisik-Mobile") == {}

    defaults = {"mtu": 1240, "dns": ["1.1.1.1", "1.0.0.1"], "keepalive": 0}
    assert run("settings-save", data=defaults) == defaults
    assert run("settings-show", "Kisik-Mobile") == {}
    assert module["new_subscription"](module["load_users"]())["settings"] == defaults
    assert (out / "original-feed-token").read_bytes() == original_feed

    assert run("settings-save", "Kisik-Mobile", data=defaults) == defaults
    assert run("show", "Kisik-Mobile") == previous_card
    assert (out / "original-basic-token").read_bytes() == original_basic
    awg = decode((out / "original-feed-token").read_text()).decode().splitlines()[-1]
    edited_conf = decode(awg.split("://", 1)[1].split("#", 1)[0]).decode()
    assert "MTU = 1240\n" in edited_conf and "DNS = 1.1.1.1, 1.0.0.1\n" in edited_conf
    assert "PersistentKeepalive = 0\n" in edited_conf and "PrivateKey = original-key\n" in edited_conf
    packet = decode((out / "original-vpn-token.vpnuri").read_text().strip()[6:])
    rendered = json.loads(zlib.decompress(packet[4:]))
    rendered_config = json.loads(rendered["containers"][0]["awg"]["last_config"])
    assert rendered_config == {"config": edited_conf, "mtu": "1240", "persistent_keep_alive": "0"}
    assert (rendered["dns1"], rendered["dns2"]) == ("1.1.1.1", "1.0.0.1")

    run("settings-save", data={"mtu": 1300})
    assert run("settings-show", "Kisik-Mobile") == defaults
    for bad in ({"mtu": 0}, {"dns": ["::1"]}, {"keepalive": True}, {"unexpected": "field"}):
        try:
            run("settings-save", "Kisik-Mobile", data=bad)
        except SystemExit:
            pass
        else:
            raise AssertionError(bad)
    assert run("settings-show", "Kisik-Mobile") == defaults
    run("settings-save", "Kisik-Mobile", data={})
    assert (out / "original-feed-token").read_bytes() == original_feed
    assert (out / "original-vpn-token.vpnuri").read_text().strip() == link
`)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("subscription settings: %v\n%s", err, out)
	}
}
