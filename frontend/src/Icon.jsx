import React from 'react';

// Все пиктограммы интерфейса масштабируются вместе с размером шрифта.
const paths = {
  text: 'M4 5h16M4 10h16M4 15h11M4 20h8',
  upload: 'M12 16V3m-5 5 5-5 5 5M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4',
  book: 'M3 5c4-1 6-.5 9 2 3-2.5 5-3 9-2v14c-4-1-6-.5-9 2-3-2.5-5-3-9-2ZM12 7v13',
  grammar: 'M3 19 9 5l6 14M5 15h8m2-8h6m-6 5h4m-3 5h5',
  chat: 'M21 11a8 8 0 0 1-8 8H8l-5 3 1-6a8 8 0 1 1 17-5ZM8 11h.01M12 11h.01M16 11h.01',
  bulb: 'M8.5 15.5C8 14 5 12.5 5 9a7 7 0 0 1 14 0c0 3.5-3 5-3.5 6.5l-.5 1.5H9Zm1 5h5',
  sliders: 'M3 7h6m4 0h8M3 17h11m4 0h3M9 4v6m7 4v6',
  spark: 'm12 3 2.6 6.4L21 12l-6.4 2.6L12 21l-2.6-6.4L3 12l6.4-2.6Z',
  file: 'M6 2.5h8l4 4V21H6ZM14 2.5V7h4M9 12h6m-6 4h6',
  paste: 'M8 5H5v16h14V5h-3M8 3h8v4H8Zm0 10h8m-8 4h5',
  chevron: 'm6 9 6 6 6-6',
  check: 'm5 12 4.5 4.5L19 7',
  arrow: 'M4 12h16m-6-6 6 6-6 6',
  plus: 'M5 12h14M12 5v14',
  minus: 'M5 12h14',
  alert: 'M12 8v5m0 3h.01M10.3 3.8 2.6 18a2 2 0 0 0 1.8 3h15.2a2 2 0 0 0 1.8-3L13.7 3.8a2 2 0 0 0-3.4 0Z',
};

export default function Icon({name, size = 18}) {
  return <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d={paths[name]} /></svg>;
}
