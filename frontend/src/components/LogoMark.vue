<template>
  <!-- Minimalist logo mark: rounded square + simple cat face, matches build/icon.svg -->
  <svg class="h-4 w-4" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
    <rect x="1" y="1" width="22" height="22" rx="5" fill="#66C7FF" />
    <!-- simplified white cat face -->
    <g transform="translate(3,3) scale(0.18)">
      <path d="M120 40 L200 0 L220 80 Q160 40 120 40 Z" fill="#FFFFFF" opacity="0.95" transform="translate(80,0)" />
      <path d="M420 40 L340 0 L320 80 Q380 40 420 40 Z" fill="#FFFFFF" opacity="0.95" transform="translate(-80,0)" />
      <path d="M256 96 C160 96 96 160 96 256 C96 352 160 416 256 416 C352 416 416 352 416 256 C416 160 352 96 256 96 Z" fill="#FFFFFF" />
      <circle cx="192" cy="240" r="16" fill="#66C7FF" />
      <circle cx="320" cy="240" r="16" fill="#66C7FF" />
      <path d="M256 288 Q256 312 280 312" stroke="#66C7FF" stroke-width="12" stroke-linecap="round" fill="none" />
    </g>
  </svg>
</template>
