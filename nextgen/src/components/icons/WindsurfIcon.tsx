import { CSSProperties } from 'react';
import devinIcon from '../../assets/icons/devin.png';

type WindsurfIconProps = {
  className?: string;
  style?: CSSProperties;
};

/** 平台内部 id 仍为 windsurf，展示品牌已切换为 Windsurf */
export function WindsurfIcon({ className = 'nav-item-icon', style }: WindsurfIconProps) {
  return (
    <img
      src={devinIcon}
      className={className}
      style={style}
      alt="Windsurf"
      aria-hidden="true"
    />
  );
}
