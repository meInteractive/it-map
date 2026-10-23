// Server-only; extracted from the v218 Worker.
import {encoder,USERS,SESSION_TTL_SECONDS,COOKIE_NAME} from '../constants.js';

export function bytesToBase64Url(bytes) {
  let value = "";
  for (const byte of bytes) value += String.fromCharCode(byte);
  return btoa(value).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

export async function sign(secret, value) {
  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"]
  );
  const signature = await crypto.subtle.sign("HMAC", key, encoder.encode(value));
  return bytesToBase64Url(new Uint8Array(signature));
}

export function timingSafeEqual(left, right) {
  if (left.length !== right.length) return false;
  let difference = 0;
  for (let index = 0; index < left.length; index += 1) {
    difference |= left.charCodeAt(index) ^ right.charCodeAt(index);
  }
  return difference === 0;
}

export async function passwordMatches(submitted, expected, secret) {
  const [submittedHash, expectedHash] = await Promise.all([
    sign(secret, submitted),
    sign(secret, expected)
  ]);
  return timingSafeEqual(submittedHash, expectedHash);
}

export async function authenticateUser(login, password, env, secret) {
  const user = USERS[String(login || "").trim().toLowerCase()];
  if (!user || !password) return null;
  const expectedPassword = String(env?.[user.passwordEnvKey] || "");
  if (!expectedPassword) return null;
  const valid = await passwordMatches(password, expectedPassword, secret);
  return valid ? { name: user.name, role: user.role, isAdmin: user.role === "admin" } : null;
}

export function readCookie(request, name) {
  const cookieHeader = request.headers.get("cookie") || "";
  for (const part of cookieHeader.split(";")) {
    const [key, ...value] = part.trim().split("=");
    if (key === name) return value.join("=");
  }
  return null;
}

export async function createSession(secret, userName) {
  const expiresAt = Date.now() + SESSION_TTL_SECONDS * 1000;
  const encodedUser = bytesToBase64Url(encoder.encode(userName));
  const payload = `v2.${expiresAt}.${encodedUser}`;
  return `${payload}.${await sign(secret, payload)}`;
}

export async function readSession(request, secret) {
  const token = readCookie(request, COOKIE_NAME);
  if (!token) return null;
  const parts = token.split(".");
  if (parts.length !== 4 || parts[0] !== "v2") return null;
  const expiresAt = Number(parts[1]);
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) return null;
  const payload = parts.slice(0, 3).join(".");
  const expectedSignature = await sign(secret, payload);
  if (!timingSafeEqual(parts[3], expectedSignature)) return null;
  let userName = "";
  try { userName = new TextDecoder().decode(base64UrlToBytes(parts[2])); } catch { return null; }
  const user = USERS[userName.toLowerCase()];
  return user ? { name: user.name, role: user.role, isAdmin: user.role === "admin" } : null;
}

export function base64UrlToBytes(value) {
  const normalized = String(value || "").replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized + "=".repeat((4 - (normalized.length % 4)) % 4);
  const binary = atob(padded);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}
