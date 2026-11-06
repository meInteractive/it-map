import React from 'react';
import {createRoot} from 'react-dom/client';
import Scheduler from '../../frontend/features/settings/Scheduler';
import HHRoleSettings from '../../frontend/features/settings/HHRoleSettings';
import '../../frontend/styles/app.css';
import '../../frontend/styles/react.css';
import '../../frontend/styles/parser.css';
import '../../web/layout.css';
createRoot(document.getElementById('root')!).render(<Scheduler roles={<HHRoleSettings/>}/>);
