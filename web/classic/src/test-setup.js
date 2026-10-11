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
import '@testing-library/jest-dom/vitest';
import i18next from 'i18next';

await i18next.init({
  lng: 'en',
  fallbackLng: 'en',
  keySeparator: false,
  resources: { en: { translation: {} } },
});

Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
  configurable: true,
  value: () => new DOMRect(0, 0, 0, 0),
});

const canvasContext = new Proxy(
  {},
  {
    get: (_target, property) => {
      if (property === 'canvas') return document.createElement('canvas');
      return () => undefined;
    },
    set: () => true,
  },
);

Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
  configurable: true,
  value: () => canvasContext,
});

Object.defineProperty(window, 'matchMedia', {
  configurable: true,
  value: (query) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  }),
});

globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

globalThis.IntersectionObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
