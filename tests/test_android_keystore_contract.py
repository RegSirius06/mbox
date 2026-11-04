from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]


def test_profile_persists_psk_only_as_keystore_ciphertext():
    src=(ROOT/'android/app/src/main/java/io/mbox/android/ProfileStore.java').read_text()
    assert 'CredentialVault.encryptToFile' in src
    assert 'psk_encrypted_file' in src
    commit=src[src.index('public static String commitCandidate'):src.index('private static void cleanupOldGenerations')]
    assert 'writePrivateFile(psk,' not in commit


def test_vault_uses_android_keystore_aes_gcm():
    src=(ROOT/'android/app/src/main/java/io/mbox/android/CredentialVault.java').read_text()
    assert 'AndroidKeyStore' in src
    assert 'AES/GCM/NoPadding' in src
    assert 'setKeySize(256)' in src


def test_plaintext_runtime_credential_is_ephemeral():
    src=(ROOT/'android/app/src/main/java/io/mbox/android/MBoxVpnService.java').read_text()
    assert 'materializeRuntimeConfig' in src
    assert 'try (ProfileStore.RuntimeMaterialization runtimeProfile = ProfileStore.materializeRuntimeConfig(this))' in src


def test_stale_plaintext_runtime_materializations_are_cleaned():
    src=(ROOT/'android/app/src/main/java/io/mbox/android/ProfileStore.java').read_text()
    assert 'cleanupRuntimeMaterializations(c);' in src
    assert 'endsWith("-runtime")' in src
