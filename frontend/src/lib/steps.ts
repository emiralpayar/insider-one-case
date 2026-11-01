// The campaign flow as the stepper shows it. "Gönderim" is the live
// dashboard, reached only by approving.
export type Step = 'brief' | 'audience' | 'content' | 'approve'

export const FLOW_STEPS: { key: Step | 'send'; title: string; sub: string }[] = [
  { key: 'brief', title: 'Brief', sub: 'Ne göndermek istiyorsunuz?' },
  { key: 'audience', title: 'Kitle', sub: 'Kural ve kişi sayıları' },
  { key: 'content', title: 'İçerik', sub: 'SMS ve e-posta' },
  { key: 'approve', title: 'Onay', sub: 'Özet ve onay' },
  { key: 'send', title: 'Gönderim', sub: 'Canlı izleme' },
]
