// A minimal stand-in for authlib-injector and Mojang's authlib (design 02 §6 item 2):
// it signs in, joins a server and checks hasJoined the way a launcher and a game server
// do, and applies the checks authlib-injector makes before a game trusts the site:
// the metadata key parses, every profile property is signed with it (SHA1withRSA), and
// texture URLs are on a domain from skinDomains. Run with Java 11+ as a single file:
//
//   java Harness.java <api root> <username> <password>
//
// It prints one line per step and exits 1 on the first failure.

import java.math.BigInteger;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.security.KeyFactory;
import java.security.MessageDigest;
import java.security.PublicKey;
import java.security.SecureRandom;
import java.security.Signature;
import java.security.spec.X509EncodedKeySpec;
import java.util.ArrayList;
import java.util.Base64;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

@SuppressWarnings("unchecked")
public class Harness {
    static final HttpClient http = HttpClient.newHttpClient();
    static String root;

    public static void main(String[] args) throws Exception {
        if (args.length != 3) {
            fail("usage: java Harness.java <api root> <username> <password>");
        }
        root = args[0].replaceAll("/+$", "");

        // authlib-injector: metadata, the signing key and the skin domains.
        Map<String, Object> meta = obj(get(root + "/").body);
        PublicKey key = publicKey((String) meta.get("signaturePublickey"));
        List<Object> skinDomains = list(meta.get("skinDomains"));
        ok("metadata: " + ((Map<?, ?>) meta.get("meta")).get("implementationName") + ", key " + key.getAlgorithm()
                + ", skin domains " + skinDomains);

        // Launcher: sign in. One player selects itself; with several, the launcher picks one.
        String clientToken = HexFormat.of().formatHex(random(16));
        Map<String, Object> auth = obj(post("/authserver/authenticate", "{\"agent\":{\"name\":\"Minecraft\",\"version\":1},"
                + "\"username\":" + quote(args[1]) + ",\"password\":" + quote(args[2]) + ",\"clientToken\":" + quote(clientToken)
                + ",\"requestUser\":true}", 200).body);
        String token = (String) auth.get("accessToken");
        if (!clientToken.equals(auth.get("clientToken"))) {
            fail("authenticate: the clientToken did not come back");
        }
        Map<String, Object> profile = (Map<String, Object>) auth.get("selectedProfile");
        if (profile == null) {
            List<Object> available = list(auth.get("availableProfiles"));
            if (available.isEmpty()) {
                fail("authenticate: no profiles");
            }
            profile = (Map<String, Object>) available.get(0);
            Map<String, Object> refreshed = obj(post("/authserver/refresh", "{\"accessToken\":" + quote(token) + ",\"clientToken\":"
                    + quote(clientToken) + ",\"selectedProfile\":{\"id\":" + quote((String) profile.get("id")) + ",\"name\":"
                    + quote((String) profile.get("name")) + "}}", 200).body);
            token = (String) refreshed.get("accessToken");
        }
        String uuid = (String) profile.get("id"), name = (String) profile.get("name");
        ok("authenticate: " + name + " " + uuid);
        post("/authserver/validate", "{\"accessToken\":" + quote(token) + ",\"clientToken\":" + quote(clientToken) + "}", 204);
        ok("validate");

        // Client: join with the server hash Minecraft computes (a signed SHA-1 in hex, so
        // about half are negative), then the server asks hasJoined.
        MessageDigest sha1 = MessageDigest.getInstance("SHA-1");
        sha1.update(random(16)); // the shared secret
        sha1.update(key.getEncoded()); // any server key; only the hash travels
        String serverId = new BigInteger(sha1.digest()).toString(16);
        post("/sessionserver/session/minecraft/join", "{\"accessToken\":" + quote(token) + ",\"selectedProfile\":" + quote(uuid)
                + ",\"serverId\":" + quote(serverId) + "}", 204);
        ok("join: serverId " + serverId);
        Resp joined = get(root + "/sessionserver/session/minecraft/hasJoined?username=" + enc(name) + "&serverId=" + enc(serverId));
        if (joined.status != 200) {
            fail("hasJoined: " + joined.status + " " + joined.body);
        }
        checkProfile(obj(joined.body), uuid, name, key, skinDomains, true);
        ok("hasJoined: signed profile verified");
        if (get(root + "/sessionserver/session/minecraft/hasJoined?username=" + enc(name) + "&serverId=other").status != 204) {
            fail("hasJoined with another serverId must be 204");
        }
        ok("hasJoined with another serverId: 204");

        // Server: fill in skins of other players by UUID, signed.
        Resp byUUID = get(root + "/sessionserver/session/minecraft/profile/" + uuid + "?unsigned=false");
        checkProfile(obj(byUUID.body), uuid, name, key, skinDomains, true);
        ok("profile by UUID: signed profile verified");
        List<Object> found = list(parse(post("/api/profiles/minecraft", "[" + quote(name.toUpperCase()) + "]", 200).body));
        if (found.size() != 1 || !uuid.equals(((Map<?, ?>) found.get(0)).get("id"))) {
            fail("profiles by name: " + found);
        }
        ok("profiles by name (any case)");

        post("/authserver/invalidate", "{\"accessToken\":" + quote(token) + ",\"clientToken\":" + quote(clientToken) + "}", 204);
        post("/authserver/validate", "{\"accessToken\":" + quote(token) + "}", 403);
        ok("invalidate");
        System.out.println("ALL OK");
    }

    static void checkProfile(Map<String, Object> p, String uuid, String name, PublicKey key, List<Object> skinDomains, boolean signed)
            throws Exception {
        if (!uuid.equals(p.get("id")) || !name.equals(p.get("name"))) {
            fail("profile is " + p.get("id") + " " + p.get("name") + ", want " + uuid + " " + name);
        }
        boolean textures = false;
        for (Object o : list(p.get("properties"))) {
            Map<String, Object> prop = (Map<String, Object>) o;
            String value = (String) prop.get("value"), sig = (String) prop.get("signature");
            if (signed) {
                if (sig == null) {
                    fail("property " + prop.get("name") + " is not signed");
                }
                Signature v = Signature.getInstance("SHA1withRSA");
                v.initVerify(key);
                v.update(value.getBytes(StandardCharsets.UTF_8));
                if (!v.verify(Base64.getDecoder().decode(sig))) {
                    fail("property " + prop.get("name") + ": signature does not verify with the metadata key");
                }
            }
            if (!"textures".equals(prop.get("name"))) {
                continue;
            }
            textures = true;
            Map<String, Object> t = obj(new String(Base64.getDecoder().decode(value), StandardCharsets.UTF_8));
            if (!uuid.equals(t.get("profileId")) || !name.equals(t.get("profileName"))) {
                fail("textures property names " + t.get("profileId") + " " + t.get("profileName"));
            }
            Map<String, Object> tx = (Map<String, Object>) t.get("textures");
            for (Map.Entry<String, Object> e : tx.entrySet()) {
                String url = (String) ((Map<?, ?>) e.getValue()).get("url");
                String host = URI.create(url).getHost();
                if (!allowed(host, skinDomains)) {
                    fail(e.getKey() + " " + url + ": authlib-injector refuses textures from " + host);
                }
                Resp file = get(url);
                String hash = url.substring(url.lastIndexOf('/') + 1);
                String sum = HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(file.bytes));
                if (file.status != 200 || !sum.equals(hash)) {
                    fail(e.getKey() + " " + url + ": " + file.status + ", sha256 " + sum);
                }
            }
        }
        if (!textures) {
            fail("no textures property");
        }
    }

    // authlib-injector's whitelist: ".example.com" matches subdomains, "example.com" itself.
    static boolean allowed(String host, List<Object> domains) {
        for (Object d : domains) {
            String s = (String) d;
            if (s.startsWith(".") ? host.endsWith(s) : host.equals(s)) {
                return true;
            }
        }
        return false;
    }

    static PublicKey publicKey(String pem) throws Exception {
        String b64 = pem.replaceAll("-----[A-Z ]+-----", "").replaceAll("\\s", "");
        return KeyFactory.getInstance("RSA").generatePublic(new X509EncodedKeySpec(Base64.getDecoder().decode(b64)));
    }

    record Resp(int status, String body, byte[] bytes) {}

    static Resp get(String url) throws Exception {
        HttpResponse<byte[]> r = http.send(HttpRequest.newBuilder(URI.create(url)).GET().build(), HttpResponse.BodyHandlers.ofByteArray());
        return new Resp(r.statusCode(), new String(r.body(), StandardCharsets.UTF_8), r.body());
    }

    static Resp post(String path, String body, int want) throws Exception {
        HttpResponse<byte[]> r = http.send(HttpRequest.newBuilder(URI.create(root + path)).header("Content-Type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofByteArray());
        Resp resp = new Resp(r.statusCode(), new String(r.body(), StandardCharsets.UTF_8), r.body());
        if (resp.status != want) {
            fail("POST " + path + ": " + resp.status + " " + resp.body + ", want " + want);
        }
        return resp;
    }

    static byte[] random(int n) {
        byte[] b = new byte[n];
        new SecureRandom().nextBytes(b);
        return b;
    }

    static String enc(String s) {
        return URLEncoder.encode(s, StandardCharsets.UTF_8);
    }

    static void ok(String s) {
        System.out.println("ok   " + s);
    }

    static void fail(String s) {
        System.out.println("FAIL " + s);
        System.exit(1);
    }

    // A small JSON reader: enough for Yggdrasil answers.
    static Map<String, Object> obj(String s) {
        Object v = parse(s);
        if (!(v instanceof Map)) {
            fail("not a JSON object: " + s);
        }
        return (Map<String, Object>) v;
    }

    static List<Object> list(Object v) {
        if (!(v instanceof List)) {
            fail("not a JSON array: " + v);
        }
        return (List<Object>) v;
    }

    static Object parse(String s) {
        int[] i = {0};
        Object v = value(s, i);
        ws(s, i);
        if (i[0] != s.length()) {
            fail("trailing data in JSON: " + s);
        }
        return v;
    }

    static void ws(String s, int[] i) {
        while (i[0] < s.length() && Character.isWhitespace(s.charAt(i[0]))) {
            i[0]++;
        }
    }

    static Object value(String s, int[] i) {
        ws(s, i);
        if (i[0] >= s.length()) {
            fail("unexpected end of JSON");
        }
        char c = s.charAt(i[0]);
        if (c == '{') {
            Map<String, Object> m = new LinkedHashMap<>();
            i[0]++;
            ws(s, i);
            if (s.charAt(i[0]) == '}') {
                i[0]++;
                return m;
            }
            while (true) {
                ws(s, i);
                String k = string(s, i);
                ws(s, i);
                expect(s, i, ':');
                m.put(k, value(s, i));
                ws(s, i);
                if (s.charAt(i[0]) == ',') {
                    i[0]++;
                    continue;
                }
                expect(s, i, '}');
                return m;
            }
        }
        if (c == '[') {
            List<Object> l = new ArrayList<>();
            i[0]++;
            ws(s, i);
            if (s.charAt(i[0]) == ']') {
                i[0]++;
                return l;
            }
            while (true) {
                l.add(value(s, i));
                ws(s, i);
                if (s.charAt(i[0]) == ',') {
                    i[0]++;
                    continue;
                }
                expect(s, i, ']');
                return l;
            }
        }
        if (c == '"') {
            return string(s, i);
        }
        for (String lit : new String[] {"true", "false", "null"}) {
            if (s.startsWith(lit, i[0])) {
                i[0] += lit.length();
                return lit.equals("null") ? null : Boolean.valueOf(lit);
            }
        }
        int start = i[0];
        while (i[0] < s.length() && "+-0123456789.eE".indexOf(s.charAt(i[0])) >= 0) {
            i[0]++;
        }
        if (start == i[0]) {
            fail("bad JSON at " + start + ": " + s);
        }
        return Double.valueOf(s.substring(start, i[0]));
    }

    static void expect(String s, int[] i, char c) {
        if (s.charAt(i[0]) != c) {
            fail("JSON: want '" + c + "' at " + i[0] + ": " + s);
        }
        i[0]++;
    }

    static String string(String s, int[] i) {
        expect(s, i, '"');
        StringBuilder b = new StringBuilder();
        while (true) {
            char c = s.charAt(i[0]++);
            if (c == '"') {
                return b.toString();
            }
            if (c != '\\') {
                b.append(c);
                continue;
            }
            char e = s.charAt(i[0]++);
            switch (e) {
                case 'n' -> b.append('\n');
                case 't' -> b.append('\t');
                case 'r' -> b.append('\r');
                case 'b' -> b.append('\b');
                case 'f' -> b.append('\f');
                case 'u' -> {
                    b.append((char) Integer.parseInt(s.substring(i[0], i[0] + 4), 16));
                    i[0] += 4;
                }
                default -> b.append(e);
            }
        }
    }

    static String quote(String s) {
        StringBuilder b = new StringBuilder("\"");
        for (char c : s.toCharArray()) {
            if (c == '"' || c == '\\') {
                b.append('\\');
            }
            b.append(c);
        }
        return b.append('"').toString();
    }
}
