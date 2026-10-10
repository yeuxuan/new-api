/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

let auth = null;
let revision = 0;

function isObject(value) {
  return value !== null && typeof value === 'object';
}

function sanitizeUser(value) {
  if (!isObject(value)) return null;
  const { token, access_token, refresh_token, ...user } = value;
  return user;
}

export function isAuthBundle(value) {
  return (
    isObject(value) &&
    typeof value.access_token === 'string' &&
    value.access_token.length > 0 &&
    value.token_type === 'Bearer' &&
    Number.isFinite(value.access_expires_at) &&
    value.access_expires_at > 0 &&
    isObject(value.user) &&
    Number.isInteger(value.user.id) &&
    value.user.id > 0 &&
    isObject(value.session) &&
    typeof value.session.sid === 'string' &&
    value.session.sid.length > 0
  );
}

export function isAuthTokenRotation(value) {
  return (
    isObject(value) &&
    typeof value.access_token === 'string' &&
    value.access_token.length > 0 &&
    value.token_type === 'Bearer' &&
    Number.isFinite(value.access_expires_at) &&
    value.access_expires_at > 0 &&
    isObject(value.session) &&
    typeof value.session.sid === 'string' &&
    value.session.current === true
  );
}

export function userFromAuthBundle(bundle) {
  if (!isAuthBundle(bundle)) throw new Error('Invalid authentication response');
  const user = sanitizeUser(bundle.user);
  return { ...user, session: { sid: bundle.session.sid } };
}

export function persistAuthResponse(data) {
  const user = userFromAuthBundle(data);
  auth = data;
  revision += 1;
  localStorage.setItem('user', JSON.stringify(user));
  return user;
}

export function getStoredUser() {
  const raw = localStorage.getItem('user');
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw);
    if (!isObject(parsed)) return null;

    // Migrate the old full auth response without retaining either token.
    if (isAuthBundle(parsed)) {
      const user = userFromAuthBundle(parsed);
      localStorage.setItem('user', JSON.stringify(user));
      return user;
    }

    const user = sanitizeUser(parsed);
    if (parsed.token || parsed.access_token || parsed.refresh_token) {
      localStorage.setItem('user', JSON.stringify(user));
    }
    return user;
  } catch {
    return null;
  }
}

export function getAuthState() {
  const stored = getStoredUser();
  if (auth && (!stored || stored.session?.sid !== auth.session.sid)) {
    auth = null;
    revision += 1;
  }
  return auth;
}

export function getAuthRevision() {
  return revision;
}

export function clearAuthSession() {
  auth = null;
  revision += 1;
  localStorage.removeItem('user');
}

export function applyAuthRotation(rotation) {
  const current = getAuthState();
  if (
    !isAuthTokenRotation(rotation) ||
    !current ||
    current.session.sid !== rotation.session.sid
  ) {
    throw new Error('Invalid authentication rotation response');
  }
  auth = { ...current, ...rotation };
  revision += 1;
  return true;
}
