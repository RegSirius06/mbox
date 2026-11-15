"""Static safety/regression tests for beta.4.2 multi-sub Android UI.
These are NOT a substitute for compiling on Android SDK / device tests.
"""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ANDROID = ROOT / 'android/app/src/main/java/io/mbox/android'


def test_multiple_subscription_urls_encrypted_and_independent():
    src = (ANDROID / 'SubscriptionStore.java').read_text()
    assert 'MAX_SUBSCRIPTIONS = 12' in src
    assert 'for (String id : ids(c))' in src
    assert 'CredentialVault.encryptToFile(temp, plaintext)' in src
    assert 'private static void refreshOne' in src
    assert 'static int[] refreshAll(Context c)' in src
    assert 'static void selectEndpoint(Context c, String id, int index)' in src
    assert 'static String[] subscriptionLabels(Context c)' in src
    assert 'writeEncrypted(urlFile(c,id), url)' in src
    assert 'writeEncrypted(bundleFile(c,id), doc.toString())' in src
    assert 'RANDOM.nextBytes(b)' in src
    assert 'setInstanceFollowRedirects(false)' in src
    assert 'MAX_BYTES' in src


def test_old_subscription_files_preserved_and_no_unintended_revoke():
    src = (ANDROID / 'SubscriptionStore.java').read_text()
    assert '"legacy"' in src
    assert 'return LEGACY.equals(id) ? directory(c)' in src
    assert 'static void deactivate(Context c)' in src
    assert 'static void saveDirect(Context c, String raw)' in src
    assert 'static void activateDirect(Context c)' in src
    # Removal is LOCAL only; never trigger a server API to revoke a subscription.
    assert 'static void remove(Context c, String id)' in src
    assert 'revoke-all' not in src


def test_switch_profile_only_when_vpn_stopped_and_debug_transport_hidden():
    ui = (ANDROID / 'MainActivity.java').read_text()
    assert 'final boolean manualTransport = debugLab && hasQuic;' in ui
    assert 'if (manualTransport) options.add("Transport mode");' in ui
    assert 'case "Transport mode": showTransportDialog();break;' in ui
    assert 'if (!AppRuntime.udpLabEnabled(this)) return;' in ui
    assert 'case "Subscriptions": showSubscriptions();break;' in ui
    assert 'showSubscriptionEndpoints(String id)' in ui
    assert 'SubscriptionStore.selectEndpoint(this,id,which)' in ui
    assert 'SubscriptionStore.importUrl(this, url)' in ui
    assert 'SubscriptionStore.refreshAll(this)' in ui
    assert 'SubscriptionStore.saveDirect(this, value)' in ui


def test_no_fixed_subscription_url_path():
    src = (ANDROID / 'SubscriptionStore.java').read_text()
    assert 'uri.getRawPath().matches' in src
    assert '/mbox/sub/' not in src
    assert 'https".equalsIgnoreCase(uri.getScheme())' in src
